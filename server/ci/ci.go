package ci

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/pogo-vcs/pogo/server/ci/docker"
	"gopkg.in/yaml.v3"
)

var (
	//go:embed schema.json schema.xsd
	Schemas embed.FS
)

type EventType uint8

const (
	EventTypePush EventType = iota
	EventTypeRemove
)

func (t EventType) String() string {
	switch t {
	case EventTypePush:
		return "push"
	case EventTypeRemove:
		return "remove"
	default:
		return "unknown"
	}
}

type (
	Config struct {
		// CI version
		Version int `yaml:"version" json:"version"`
		// On what events should the CI run
		On On `yaml:"on" json:"on"`
		// Tasks to run when the events are triggered
		Do []Task `yaml:"do" json:"do"`
	}
	On struct {
		// Push events
		Push *OnPush `yaml:"push,omitempty" json:"push,omitempty"`
		// Remove events
		Remove *OnRemove `yaml:"remove,omitempty" json:"remove,omitempty"`
	}
	OnPush struct {
		// Bookmark globs that, when matched a created or updated bookmark name, will trigger the CI
		Bookmarks []string `yaml:"bookmarks" json:"bookmarks"`
	}
	OnRemove struct {
		// Bookmark globs that, when matched a removed bookmark name, will trigger the CI
		Bookmarks []string `yaml:"bookmarks" json:"bookmarks"`
	}
	Task struct {
		// Type of task: "webhook" or "container"
		Type string `yaml:"type" json:"type"`
		// Webhook-specific fields
		Webhook *WebhookTask `yaml:"webhook,omitempty" json:"webhook,omitempty"`
		// Container-specific fields
		Container *ContainerTask `yaml:"container,omitempty" json:"container,omitempty"`
	}
	WebhookTask struct {
		// Url to make a HTTP request to
		Url string `yaml:"url" json:"url"`
		// HTTP Method to use for the request
		Method string `yaml:"method" json:"method"`
		// Headers to send with the request
		Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
		// Body to send with the request
		Body string `yaml:"body,omitempty" json:"body,omitempty"`
		// Retry policy to use when the request fails
		Retry *RetryPolicy `yaml:"retry_policy,omitempty" json:"retry_policy,omitempty"`
	}
	ContainerTask struct {
		// Image to use, either from registry (e.g. "alpine:latest") or Dockerfile path in repo (e.g. "./Dockerfile")
		Image string `yaml:"image" json:"image"`
		// Commands to run inside the container
		Commands []string `yaml:"commands,omitempty" json:"commands,omitempty"`
		// Environment variables to set in the container
		Environment map[string]string `yaml:"environment,omitempty" json:"environment,omitempty"`
		// Working directory inside the container
		WorkingDir string `yaml:"working_dir,omitempty" json:"working_dir,omitempty"`
		// Services to start alongside the main container
		Services []Service `yaml:"services,omitempty" json:"services,omitempty"`
	}
	Service struct {
		// Name of the service (used for network hostname)
		Name string `yaml:"name" json:"name"`
		// Image to use for the service
		Image string `yaml:"image" json:"image"`
		// Environment variables for the service
		Environment map[string]string `yaml:"environment,omitempty" json:"environment,omitempty"`
	}
	RetryPolicy struct {
		// Maximum number of attempts to make before giving up including the first attempt
		MaxAttempts int `yaml:"max_attempts" json:"max_attempts"`
	}
)

type TaskExecutionResult struct {
	ConfigFilename string
	EventType      EventType
	Rev            string
	Pattern        string
	Reason         string
	TaskType       string
	StatusCode     int
	Success        bool
	StartedAt      time.Time
	FinishedAt     time.Time
	Log            string
}

type Event struct {
	// Rev is the revision name that triggered this event, might be a bookmark or change name
	Rev string
	// ArchiveUrl is the url where the archive of this revision can be found (requires authentication if private repo)
	ArchiveUrl string
	// Author is the username of the author who created the change
	Author string
	// Description is the description of the change
	Description string
}

func makeUnmarshalConfigFuncs(secrets map[string]string) template.FuncMap {
	return template.FuncMap{
		"toUpper": strings.ToUpper,
		"toLower": strings.ToLower,
		"trim":    strings.TrimSpace,
		"btoa":    func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) },
		"atob":    func(s string) string { b, _ := base64.StdEncoding.DecodeString(s); return string(b) },
		"secret": func(key string) string {
			if secrets == nil {
				return ""
			}
			return secrets[key]
		},
	}
}

func UnmarshalConfig(yamlString []byte, data Event) (*Config, error) {
	return UnmarshalConfigWithSecrets(yamlString, data, nil)
}

func UnmarshalConfigWithSecrets(yamlString []byte, data Event, secrets map[string]string) (*Config, error) {
	var c Config

	t, err := template.New("ci_config").
		Funcs(makeUnmarshalConfigFuncs(secrets)).
		Parse(string(yamlString))

	if err != nil {
		if yamlErr := yaml.Unmarshal(yamlString, &c); yamlErr != nil {
			return nil, errors.Join(
				fmt.Errorf("unmarshal plain config: %w", yamlErr),
				fmt.Errorf("parse template: %w", err),
			)
		}
		return &c, nil
	}

	sb := strings.Builder{}

	if err := t.Execute(&sb, data); err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal([]byte(sb.String()), &c); err != nil {
		return nil, err
	}

	return &c, nil
}

type Executor struct {
	httpClient     *http.Client
	dockerClient   docker.Client
	repoContentDir string
	secrets        map[string]string
}

func NewExecutor() *Executor {
	dockerClient, _ := docker.NewClient()
	return &Executor{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		dockerClient: dockerClient,
	}
}

func (e *Executor) SetRepoContentDir(dir string) {
	e.repoContentDir = dir
}

func (e *Executor) SetSecrets(secrets map[string]string) {
	e.secrets = secrets
}

func (e *Executor) ExecuteForBookmarkEvent(ctx context.Context, configFiles map[string][]byte, event Event, eventType EventType) ([]TaskExecutionResult, error) {
	var allResults []TaskExecutionResult

	for filename, configData := range configFiles {
		if !isYAMLFile(filename) {
			continue
		}

		config, err := UnmarshalConfigWithSecrets(configData, event, e.secrets)
		if err != nil {
			return allResults, fmt.Errorf("unmarshal config %s: %w", filename, err)
		}

		if eventType == EventTypePush && config.On.Push != nil {
			for _, pattern := range config.On.Push.Bookmarks {
				if matchesPattern(event.Rev, pattern) {
					reason := fmt.Sprintf("config=%s event=%s rev=%s pattern=%s", filename, eventType.String(), event.Rev, pattern)
					fmt.Printf("CI run reason: %s\n", reason)
					taskResults, execErr := e.executeTasks(ctx, config.Do)
					for i := range taskResults {
						taskResults[i].ConfigFilename = filename
						taskResults[i].EventType = eventType
						taskResults[i].Rev = event.Rev
						taskResults[i].Pattern = pattern
						taskResults[i].Reason = reason
					}
					allResults = append(allResults, taskResults...)
					if execErr != nil {
						return allResults, fmt.Errorf("execute tasks for %s: %w", filename, execErr)
					}
					break
				}
			}
		} else if eventType == EventTypeRemove && config.On.Remove != nil {
			for _, pattern := range config.On.Remove.Bookmarks {
				if matchesPattern(event.Rev, pattern) {
					reason := fmt.Sprintf("config=%s event=%s rev=%s pattern=%s", filename, eventType.String(), event.Rev, pattern)
					fmt.Printf("CI run reason: %s\n", reason)
					taskResults, execErr := e.executeTasks(ctx, config.Do)
					for i := range taskResults {
						taskResults[i].ConfigFilename = filename
						taskResults[i].EventType = eventType
						taskResults[i].Rev = event.Rev
						taskResults[i].Pattern = pattern
						taskResults[i].Reason = reason
					}
					allResults = append(allResults, taskResults...)
					if execErr != nil {
						return allResults, fmt.Errorf("execute tasks for %s: %w", filename, execErr)
					}
					break
				}
			}
		}
	}
	return allResults, nil
}

func (e *Executor) executeTasks(ctx context.Context, tasks []Task) ([]TaskExecutionResult, error) {
	var results []TaskExecutionResult
	for _, task := range tasks {
		result, err := e.executeTask(ctx, task)
		results = append(results, result)
		if err != nil {
			return results, fmt.Errorf("execute task: %w", err)
		}
	}
	return results, nil
}

func (e *Executor) executeTask(ctx context.Context, task Task) (TaskExecutionResult, error) {
	switch task.Type {
	case "webhook":
		if task.Webhook == nil {
			return TaskExecutionResult{TaskType: task.Type, Success: false}, fmt.Errorf("webhook task missing webhook configuration")
		}
		return e.executeWebhookTask(ctx, *task.Webhook)
	case "container":
		if task.Container == nil {
			return TaskExecutionResult{TaskType: task.Type, Success: false}, fmt.Errorf("container task missing container configuration")
		}
		return e.executeContainerTask(ctx, *task.Container)
	default:
		return TaskExecutionResult{TaskType: task.Type, Success: false}, fmt.Errorf("unknown task type: %s", task.Type)
	}
}

func (e *Executor) executeWebhookTask(ctx context.Context, task WebhookTask) (TaskExecutionResult, error) {
	result := TaskExecutionResult{
		TaskType:  "webhook",
		StartedAt: time.Now(),
	}

	attempts := 1
	if task.Retry != nil && task.Retry.MaxAttempts > 1 {
		attempts = task.Retry.MaxAttempts
	}

	var logBuf bytes.Buffer
	logWriter := io.MultiWriter(&logBuf, os.Stdout)
	fmt.Fprintf(logWriter, "Request: %s %s\n", task.Method, task.Url)
	if len(task.Headers) > 0 {
		fmt.Fprintln(logWriter, "Request Headers:")
		for key, value := range task.Headers {
			fmt.Fprintf(logWriter, "  %s: %s\n", key, value)
		}
	}
	if task.Body != "" {
		fmt.Fprintln(logWriter, "Request Body:")
		fmt.Fprintln(logWriter, task.Body)
	}

	var lastErr error
	var statusCode int
	for i := 0; i < attempts; i++ {
		fmt.Fprintf(logWriter, "Attempt %d/%d\n", i+1, attempts)
		code, headers, body, err := e.makeHTTPRequest(ctx, task)
		if code != 0 {
			statusCode = code
		}
		if len(headers) > 0 {
			fmt.Fprintln(logWriter, "Response Headers:")
			for key, values := range headers {
				fmt.Fprintf(logWriter, "  %s: %s\n", key, strings.Join(values, ","))
			}
		}
		if len(body) > 0 {
			fmt.Fprintln(logWriter, "Response Body:")
			fmt.Fprintln(logWriter, string(body))
		}

		if err != nil {
			lastErr = err
			fmt.Fprintf(logWriter, "Error: %v\n", err)
			if i < attempts-1 {
				time.Sleep(time.Duration(i+1) * time.Second)
			}
			continue
		}
		result.Success = true
		result.StatusCode = code
		result.FinishedAt = time.Now()
		result.Log = logBuf.String()
		return result, nil
	}

	if statusCode == 0 {
		result.StatusCode = -1
	} else {
		result.StatusCode = statusCode
	}
	result.Success = false
	result.FinishedAt = time.Now()
	result.Log = logBuf.String()
	if lastErr != nil {
		return result, lastErr
	}
	return result, fmt.Errorf("webhook task failed without response")
}

func (e *Executor) executeContainerTask(ctx context.Context, task ContainerTask) (TaskExecutionResult, error) {
	result := TaskExecutionResult{
		TaskType:  "container",
		StartedAt: time.Now(),
	}

	if e.dockerClient == nil {
		result.Log = "docker client not available"
		result.StatusCode = -1
		result.FinishedAt = time.Now()
		return result, fmt.Errorf("docker not available")
	}

	var logBuf bytes.Buffer
	logWriter := io.MultiWriter(os.Stdout, &logBuf)

	networkName := fmt.Sprintf("pogo-ci-%d", time.Now().UnixNano())

	if err := e.dockerClient.CreateNetwork(ctx, networkName); err != nil {
		result.Log = logBuf.String()
		result.StatusCode = -1
		result.FinishedAt = time.Now()
		return result, fmt.Errorf("create network: %w", err)
	}
	defer e.dockerClient.RemoveNetwork(context.Background(), networkName)

	var serviceContainers []string
	for _, service := range task.Services {
		serviceID := fmt.Sprintf("%s-%d", service.Name, time.Now().UnixNano())

		fmt.Fprintf(logWriter, "Pulling service image %s\n", service.Image)
		if err := e.dockerClient.PullImage(ctx, service.Image); err != nil {
			result.Log = logBuf.String()
			result.StatusCode = -1
			result.FinishedAt = time.Now()
			return result, fmt.Errorf("pull service image %s: %w", service.Image, err)
		}

		go func(svc Service, svcID string) {
			runOpts := docker.RunOptions{
				Image:       svc.Image,
				Name:        svc.Name,
				Environment: svc.Environment,
				NetworkName: networkName,
			}
			e.dockerClient.RunContainer(context.Background(), runOpts)
		}(service, serviceID)

		serviceContainers = append(serviceContainers, service.Name)
		time.Sleep(2 * time.Second)
	}

	defer func() {
		for _, containerName := range serviceContainers {
			e.dockerClient.StopContainer(context.Background(), containerName)
			e.dockerClient.RemoveContainer(context.Background(), containerName)
		}
	}()

	fmt.Fprintf(logWriter, "Pulling image %s\n", task.Image)
	if err := e.dockerClient.PullImage(ctx, task.Image); err != nil {
		result.Log = logBuf.String()
		result.StatusCode = -1
		result.FinishedAt = time.Now()
		return result, fmt.Errorf("pull image: %w", err)
	}

	if e.repoContentDir != "" {
		containerName := fmt.Sprintf("pogo-ci-%d", time.Now().UnixNano())

		runOpts := docker.RunOptions{
			Image:       task.Image,
			Commands:    task.Commands,
			Environment: task.Environment,
			WorkingDir:  task.WorkingDir,
			NetworkName: networkName,
			CreateOnly:  true,
			Name:        containerName,
		}

		if runOpts.WorkingDir == "" {
			runOpts.WorkingDir = "/workspace"
		}

		fmt.Fprintf(logWriter, "Creating container %s with commands: %s\n", task.Image, strings.Join(task.Commands, " "))

		if err := e.dockerClient.RunContainer(ctx, runOpts); err != nil {
			result.Log = logBuf.String()
			result.StatusCode = -1
			result.FinishedAt = time.Now()
			return result, fmt.Errorf("create container: %w", err)
		}

		fmt.Fprintf(logWriter, "Copying repository content to /workspace\n")
		if err := e.dockerClient.CopyToContainer(ctx, containerName, e.repoContentDir, "/workspace"); err != nil {
			e.dockerClient.RemoveContainer(context.Background(), containerName)
			result.Log = logBuf.String()
			result.StatusCode = -1
			result.FinishedAt = time.Now()
			return result, fmt.Errorf("copy to container: %w", err)
		}

		fmt.Fprintf(logWriter, "Starting container %s\n", task.Image)
		err := e.dockerClient.StartContainer(ctx, containerName, logWriter, logWriter)
		e.dockerClient.RemoveContainer(context.Background(), containerName)
		result.FinishedAt = time.Now()
		result.Log = logBuf.String()

		if err != nil {
			statusCode := exitCodeFromError(err)
			result.StatusCode = statusCode
			result.Success = false
			if statusCode == -1 {
				result.Log += fmt.Sprintf("\nError: %v\n", err)
			}
			return result, fmt.Errorf("start container: %w", err)
		}

		result.StatusCode = 0
		result.Success = true
		return result, nil
	}

	runOpts := docker.RunOptions{
		Image:       task.Image,
		Commands:    task.Commands,
		Environment: task.Environment,
		WorkingDir:  task.WorkingDir,
		NetworkName: networkName,
		Stdout:      logWriter,
		Stderr:      logWriter,
	}

	fmt.Fprintf(logWriter, "Running container %s with commands: %s\n", task.Image, strings.Join(task.Commands, " "))

	err := e.dockerClient.RunContainer(ctx, runOpts)
	result.FinishedAt = time.Now()
	result.Log = logBuf.String()

	if err != nil {
		statusCode := exitCodeFromError(err)
		result.StatusCode = statusCode
		result.Success = false
		if statusCode == -1 {
			result.Log += fmt.Sprintf("\nError: %v\n", err)
		}
		return result, fmt.Errorf("run container: %w", err)
	}

	result.StatusCode = 0
	result.Success = true
	return result, nil
}

func (e *Executor) makeHTTPRequest(ctx context.Context, task WebhookTask) (int, http.Header, []byte, error) {
	var body io.Reader
	if task.Body != "" {
		body = strings.NewReader(task.Body)
	}

	req, err := http.NewRequestWithContext(ctx, task.Method, task.Url, body)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("create request: %w", err)
	}

	for key, value := range task.Headers {
		req.Header.Set(key, value)
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("make request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	headers := resp.Header.Clone()

	if resp.StatusCode >= 400 {
		return resp.StatusCode, headers, bodyBytes, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return resp.StatusCode, headers, bodyBytes, nil
}

func isYAMLFile(filename string) bool {
	ext := filepath.Ext(filename)
	return ext == ".yaml" || ext == ".yml"
}

func matchesPattern(str, pattern string) bool {
	matched, _ := filepath.Match(pattern, str)
	return matched
}

func exitCodeFromError(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	var exitCoder interface{ ExitCode() int }
	if errors.As(err, &exitCoder) {
		return exitCoder.ExitCode()
	}
	return -1
}
