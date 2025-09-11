package ci

import (
	"context"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"text/template"
	"time"

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
	httpClient *http.Client
}

func NewExecutor() *Executor {
	return &Executor{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
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

func (e *Executor) makeHTTPRequest(ctx context.Context, task Task) error {
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
