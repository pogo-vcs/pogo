package ci

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestExecutor_ExecuteForBookmarkEvent(t *testing.T) {
	ctx := context.Background()

	// Set up test HTTP server
	var receivedRequests []TestRequest
	var mu sync.Mutex

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		req := TestRequest{
			Method:  r.Method,
			URL:     r.URL.String(),
			Headers: make(map[string]string),
		}

		for key, values := range r.Header {
			if len(values) > 0 {
				req.Headers[key] = values[0]
			}
		}

		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		req.Body = string(body[:n])

		receivedRequests = append(receivedRequests, req)
		w.WriteHeader(http.StatusOK)
	}))
	defer testServer.Close()

	executor := NewExecutor()

	tests := []struct {
		name        string
		configFiles map[string][]byte
		event       Event
		eventType   EventType
		want        int // expected number of HTTP requests
	}{
		{
			name: "bookmark push event matches pattern",
			configFiles: map[string][]byte{
				"ci.yaml": []byte(fmt.Sprintf(`
version: 1
on:
  push:
    bookmarks: ["main"]
do:
  - type: webhook
    webhook:
      url: %s/webhook
      method: POST
      headers:
        X-Event-Type: bookmark-push
`, testServer.URL)),
			},
			event: Event{
				Rev:        "main",
				ArchiveUrl: "https://example.com/archive",
			},
			eventType: EventTypePush,
			want:      1,
		},
		{
			name: "bookmark remove event matches pattern",
			configFiles: map[string][]byte{
				"ci.yaml": []byte(fmt.Sprintf(`
version: 1
on:
  remove:
    bookmarks: ["v*"]
do:
  - type: webhook
    webhook:
      url: %s/webhook
      method: DELETE
      headers:
        X-Event-Type: bookmark-remove
`, testServer.URL)),
			},
			event: Event{
				Rev:        "v1.0.0",
				ArchiveUrl: "https://example.com/archive",
			},
			eventType: EventTypeRemove,
			want:      1,
		},
		{
			name: "no pattern match",
			configFiles: map[string][]byte{
				"ci.yaml": []byte(fmt.Sprintf(`
version: 1
on:
  push:
    bookmarks: ["production"]
do:
  - type: webhook
    webhook:
      url: %s/webhook
      method: POST
`, testServer.URL)),
			},
			event: Event{
				Rev:        "main",
				ArchiveUrl: "https://example.com/archive",
			},
			eventType: EventTypePush,
			want:      0,
		},
		{
			name: "multiple tasks",
			configFiles: map[string][]byte{
				"ci.yaml": []byte(fmt.Sprintf(`
version: 1
on:
  push:
    bookmarks: ["*"]
do:
  - type: webhook
    webhook:
      url: %s/webhook1
      method: POST
  - type: webhook
    webhook:
      url: %s/webhook2
      method: PUT
`, testServer.URL, testServer.URL)),
			},
			event: Event{
				Rev:        "test",
				ArchiveUrl: "https://example.com/archive",
			},
			eventType: EventTypePush,
			want:      2,
		},
		{
			name: "non-yaml file ignored",
			configFiles: map[string][]byte{
				"ci.txt": []byte(fmt.Sprintf(`
version: 1
on:
  push:
    bookmarks: ["main"]
do:
  - type: webhook
    webhook:
      url: %s/webhook
      method: POST
`, testServer.URL)),
			},
			event: Event{
				Rev:        "main",
				ArchiveUrl: "https://example.com/archive",
			},
			eventType: EventTypePush,
			want:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset received requests
			mu.Lock()
			receivedRequests = nil
			mu.Unlock()

			results, err := executor.ExecuteForBookmarkEvent(ctx, tt.configFiles, tt.event, tt.eventType)
			if err != nil {
				t.Errorf("ExecuteForBookmarkEvent() error = %v", err)
				return
			}
			if len(results) != tt.want {
				t.Errorf("ExecuteForBookmarkEvent() got %d results, want %d", len(results), tt.want)
			}

			mu.Lock()
			got := len(receivedRequests)
			mu.Unlock()

			if got != tt.want {
				t.Errorf("ExecuteForBookmarkEvent() got %d requests, want %d", got, tt.want)
			}
		})
	}
}

func TestExecutor_Retry(t *testing.T) {
	ctx := context.Background()

	var requestCount int
	var mu sync.Mutex

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		count := requestCount
		mu.Unlock()

		// Fail the first 2 requests, succeed on the 3rd
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer testServer.Close()

	executor := NewExecutor()

	configFiles := map[string][]byte{
		"ci.yaml": []byte(fmt.Sprintf(`
version: 1
on:
  push:
    bookmarks: ["main"]
do:
  - type: webhook
    webhook:
      url: %s/webhook
      method: POST
      retry_policy:
        max_attempts: 3
`, testServer.URL)),
	}

	event := Event{
		Rev:        "main",
		ArchiveUrl: "https://example.com/archive",
	}

	results, err := executor.ExecuteForBookmarkEvent(ctx, configFiles, event, EventTypePush)
	if err != nil {
		t.Errorf("ExecuteForBookmarkEvent() should succeed with retry, got error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Success {
		t.Errorf("expected task to succeed after retries")
	}
	if results[0].StatusCode != http.StatusOK {
		t.Errorf("expected status code %d, got %d", http.StatusOK, results[0].StatusCode)
	}

	mu.Lock()
	finalCount := requestCount
	mu.Unlock()

	if finalCount != 3 {
		t.Errorf("Expected 3 requests (2 failures + 1 success), got %d", finalCount)
	}
}

func TestExecutor_RetryFails(t *testing.T) {
	ctx := context.Background()

	var requestCount int
	var mu sync.Mutex

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()

		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer testServer.Close()

	executor := NewExecutor()

	configFiles := map[string][]byte{
		"ci.yaml": []byte(fmt.Sprintf(`
version: 1
on:
  push:
    bookmarks: ["main"]
do:
  - type: webhook
    webhook:
      url: %s/webhook
      method: POST
      retry_policy:
        max_attempts: 2
`, testServer.URL)),
	}

	event := Event{
		Rev:        "main",
		ArchiveUrl: "https://example.com/archive",
	}

	results, err := executor.ExecuteForBookmarkEvent(ctx, configFiles, event, EventTypePush)
	if err == nil {
		t.Error("ExecuteForBookmarkEvent() should fail after max retries")
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Success {
		t.Error("expected task to report failure")
	}
	if results[0].StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status code %d, got %d", http.StatusInternalServerError, results[0].StatusCode)
	}

	mu.Lock()
	finalCount := requestCount
	mu.Unlock()

	if finalCount != 2 {
		t.Errorf("Expected 2 requests (max attempts), got %d", finalCount)
	}
}

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		str     string
		pattern string
		want    bool
	}{
		{"main", "main", true},
		{"main", "mai*", true},
		{"main", "*ain", true},
		{"main", "*", true},
		{"v1.0.0", "v*", true},
		{"v1.0.0", "v1.*", true},
		{"production", "prod*", true},
		{"main", "test", false},
		{"v1.0.0", "test*", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s-%s", tt.str, tt.pattern), func(t *testing.T) {
			got := matchesPattern(tt.str, tt.pattern)
			if got != tt.want {
				t.Errorf("matchesPattern(%q, %q) = %v, want %v", tt.str, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestUnmarshalConfigWithTemplating(t *testing.T) {
	configYAML := []byte(`
version: 1
on:
  push:
    bookmarks: ["{{ .Rev }}"]
do:
  - type: webhook
    webhook:
      url: https://api.example.com/webhook
      method: POST
      body: |
        {
          "event": "bookmark_push",
          "bookmark": "{{ .Rev }}",
          "archive_url": "{{ .ArchiveUrl }}"
        }
`)

	event := Event{
		Rev:        "main",
		ArchiveUrl: "https://example.com/archive.zip",
	}

	config, err := UnmarshalConfig(configYAML, event)
	if err != nil {
		t.Fatalf("UnmarshalConfig() error = %v", err)
	}

	if config.On.Push.Bookmarks[0] != "main" {
		t.Errorf("Expected bookmark pattern 'main', got %q", config.On.Push.Bookmarks[0])
	}

	if config.Do[0].Webhook == nil {
		t.Fatal("Expected webhook task but got nil")
	}

	expectedBody := `{
  "event": "bookmark_push",
  "bookmark": "main",
  "archive_url": "https://example.com/archive.zip"
}`

	if strings.TrimSpace(config.Do[0].Webhook.Body) != strings.TrimSpace(expectedBody) {
		t.Errorf("Expected body:\n%s\nGot:\n%s", expectedBody, config.Do[0].Webhook.Body)
	}
}

type TestRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    string
}
