package ci

import (
	"context"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
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

type Event struct {
	// Rev is the revision name that triggered this event, might be a bookmark or change name
	Rev string
	// ArchiveUrl is the url where the archive of this revision can be found (requires authentication if private repo)
	ArchiveUrl string
}

var unmarshalConfigFuncs = template.FuncMap{
	"toUpper": strings.ToUpper,
	"toLower": strings.ToLower,
	"trim":    strings.TrimSpace,
	"btoa":    func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) },
	"atob":    func(s string) string { b, _ := base64.StdEncoding.DecodeString(s); return string(b) },
}

func UnmarshalConfig(yamlString []byte, data Event) (*Config, error) {
	var c Config

	t, err := template.New("ci_config").
		Funcs(unmarshalConfigFuncs).
		Parse(string(yamlString))

	if err != nil {
		if yamlErr := yaml.Unmarshal(yamlString, &c); yamlErr != nil {
			return nil, errors.Join(
				fmt.Errorf("unmarshal plain config: %w", yamlErr),
				fmt.Errorf("parse template: %w", err),
			)
		}
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

func (e *Executor) ExecuteForBookmarkEvent(ctx context.Context, configFiles map[string][]byte, event Event, eventType EventType) error {
	for filename, configData := range configFiles {
		if !isYAMLFile(filename) {
			continue
		}

		config, err := UnmarshalConfig(configData, event)
		if err != nil {
			return fmt.Errorf("unmarshal config %s: %w", filename, err)
		}

		if eventType == EventTypePush && config.On.Push != nil {
			for _, pattern := range config.On.Push.Bookmarks {
				if matchesPattern(event.Rev, pattern) {
					if err := e.executeTasks(ctx, config.Do); err != nil {
						return fmt.Errorf("execute tasks for %s: %w", filename, err)
					}
					break
				}
			}
		} else if eventType == EventTypeRemove && config.On.Remove != nil {
			for _, pattern := range config.On.Remove.Bookmarks {
				if matchesPattern(event.Rev, pattern) {
					if err := e.executeTasks(ctx, config.Do); err != nil {
						return fmt.Errorf("execute tasks for %s: %w", filename, err)
					}
					break
				}
			}
		}
	}
	return nil
}

func (e *Executor) executeTasks(ctx context.Context, tasks []Task) error {
	for _, task := range tasks {
		if err := e.executeTask(ctx, task); err != nil {
			return fmt.Errorf("execute task: %w", err)
		}
	}
	return nil
}

func (e *Executor) executeTask(ctx context.Context, task Task) error {
	switch task.Type {
	case "webhook":
		if task.Webhook == nil {
			return fmt.Errorf("webhook task missing webhook configuration")
		}
		return e.executeWebhookTask(ctx, *task.Webhook)
	case "container":
		if task.Container == nil {
			return fmt.Errorf("container task missing container configuration")
		}
		return e.executeContainerTask(ctx, *task.Container)
	default:
		return fmt.Errorf("unknown task type: %s", task.Type)
	}
}

func (e *Executor) executeWebhookTask(ctx context.Context, task WebhookTask) error {
	attempts := 1
	if task.Retry != nil && task.Retry.MaxAttempts > 1 {
		attempts = task.Retry.MaxAttempts
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := e.makeHTTPRequest(ctx, task); err != nil {
			lastErr = err
			if i < attempts-1 {
				time.Sleep(time.Duration(i+1) * time.Second)
			}
			continue
		}
		return nil
	}
	return lastErr
}

func (e *Executor) executeContainerTask(ctx context.Context, task ContainerTask) error {
	if e.dockerClient == nil {
		return fmt.Errorf("docker not available")
	}

	networkName := fmt.Sprintf("pogo-ci-%d", time.Now().UnixNano())

	if err := e.dockerClient.CreateNetwork(ctx, networkName); err != nil {
		return fmt.Errorf("create network: %w", err)
	}
	defer e.dockerClient.RemoveNetwork(context.Background(), networkName)

	var serviceContainers []string
	for _, service := range task.Services {
		serviceID := fmt.Sprintf("%s-%d", service.Name, time.Now().UnixNano())

		if err := e.dockerClient.PullImage(ctx, service.Image); err != nil {
			return fmt.Errorf("pull service image %s: %w", service.Image, err)
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

	if err := e.dockerClient.PullImage(ctx, task.Image); err != nil {
		return fmt.Errorf("pull image: %w", err)
	}

	runOpts := docker.RunOptions{
		Image:       task.Image,
		Commands:    task.Commands,
		Environment: task.Environment,
		WorkingDir:  task.WorkingDir,
		NetworkName: networkName,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
	}

	if e.repoContentDir != "" {
		runOpts.Volumes = map[string]string{
			e.repoContentDir: "/workspace",
		}
		if runOpts.WorkingDir == "" {
			runOpts.WorkingDir = "/workspace"
		}
	}

	if err := e.dockerClient.RunContainer(ctx, runOpts); err != nil {
		return fmt.Errorf("run container: %w", err)
	}

	return nil
}

func (e *Executor) makeHTTPRequest(ctx context.Context, task WebhookTask) error {
	var body io.Reader
	if task.Body != "" {
		body = strings.NewReader(task.Body)
	}

	req, err := http.NewRequestWithContext(ctx, task.Method, task.Url, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	for key, value := range task.Headers {
		req.Header.Set(key, value)
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

func isYAMLFile(filename string) bool {
	ext := filepath.Ext(filename)
	return ext == ".yaml" || ext == ".yml"
}

func matchesPattern(str, pattern string) bool {
	matched, _ := filepath.Match(pattern, str)
	return matched
}
