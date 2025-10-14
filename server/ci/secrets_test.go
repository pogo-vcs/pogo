package ci

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestExecutor_WebhookWithSecrets(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	expectedSecret := "my-secret-token-12345"
	var receivedBody string
	var mu sync.Mutex

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		w.WriteHeader(http.StatusOK)
	}))
	defer testServer.Close()

	executor := NewExecutor()
	executor.SetSecrets(map[string]string{
		"DEPLOY_TOKEN": expectedSecret,
	})

	configYAML := fmt.Sprintf(`
version: 1
on:
  push:
    bookmarks: ["main"]
do:
  - type: webhook
    webhook:
      url: %s/webhook
      method: POST
      body: token={{ secret "DEPLOY_TOKEN" }}
`, testServer.URL)

	configFiles := map[string][]byte{
		"ci.yaml": []byte(configYAML),
	}

	event := Event{
		Rev:         "main",
		ArchiveUrl:  "https://example.com/archive",
		Author:      "testuser",
		Description: "Secret test",
	}

	results, err := executor.ExecuteForBookmarkEvent(ctx, configFiles, event, EventTypePush)
	if err != nil {
		t.Fatalf("ExecuteForBookmarkEvent() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Success {
		t.Fatal("expected webhook task to succeed")
	}
	if results[0].StatusCode != http.StatusOK {
		t.Fatalf("expected status code %d, got %d", http.StatusOK, results[0].StatusCode)
	}

	mu.Lock()
	actualBody := receivedBody
	mu.Unlock()

	if actualBody != "token="+expectedSecret {
		t.Errorf("Expected body 'token=%s', got '%s'", expectedSecret, actualBody)
	}
}

func TestExecutor_WebhookWithSecretsDefaultEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var receivedBody string
	var mu sync.Mutex

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		w.WriteHeader(http.StatusOK)
	}))
	defer testServer.Close()

	executor := NewExecutor()
	executor.SetSecrets(map[string]string{})

	configYAML := fmt.Sprintf(`
version: 1
on:
  push:
    bookmarks: ["main"]
do:
  - type: webhook
    webhook:
      url: %s/webhook
      method: POST
      body: token={{ secret "MISSING_SECRET" }}
`, testServer.URL)

	configFiles := map[string][]byte{
		"ci.yaml": []byte(configYAML),
	}

	event := Event{
		Rev:         "main",
		ArchiveUrl:  "https://example.com/archive",
		Author:      "testuser",
		Description: "Secret test",
	}

	results, err := executor.ExecuteForBookmarkEvent(ctx, configFiles, event, EventTypePush)
	if err != nil {
		t.Fatalf("ExecuteForBookmarkEvent() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Success {
		t.Fatal("expected webhook task to succeed with empty secret")
	}
	if results[0].StatusCode != http.StatusOK {
		t.Fatalf("expected status code %d, got %d", http.StatusOK, results[0].StatusCode)
	}

	mu.Lock()
	actualBody := receivedBody
	mu.Unlock()

	if actualBody != "token=" {
		t.Errorf("Expected body 'token=' (empty secret), got '%s'", actualBody)
	}
}

func TestExecutor_ContainerWithSecrets(t *testing.T) {
	t.Parallel()
	if !isDockerAvailableForTest() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()

	expectedSecret := "container-secret-value-xyz"
	var receivedBody string
	var mu sync.Mutex

	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		w.WriteHeader(http.StatusOK)
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	defer server.Close()

	executor := NewExecutor()
	executor.SetSecrets(map[string]string{
		"API_KEY": expectedSecret,
	})

	hostIP := getHostIPForTest()
	testURL := fmt.Sprintf("http://%s:%d", hostIP, port)

	configYAML := fmt.Sprintf(`
version: 1
on:
  push:
    bookmarks: ["main"]
do:
  - type: container
    container:
      image: alpine:latest
      commands:
        - sh
        - -c
        - apk add --no-cache curl && curl -X POST -d 'secret={{ secret "API_KEY" }}' %s/callback
`, testURL)

	configFiles := map[string][]byte{
		"ci.yaml": []byte(configYAML),
	}

	event := Event{
		Rev:         "main",
		ArchiveUrl:  "https://example.com/archive",
		Author:      "testuser",
		Description: "Secret test",
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	results, err := executor.ExecuteForBookmarkEvent(ctx, configFiles, event, EventTypePush)
	if err != nil {
		t.Fatalf("ExecuteForBookmarkEvent() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Success {
		t.Fatal("expected container task to succeed")
	}
	if results[0].StatusCode != 0 {
		t.Fatalf("expected container exit code 0, got %d", results[0].StatusCode)
	}

	mu.Lock()
	actualBody := receivedBody
	mu.Unlock()

	if actualBody != "secret="+expectedSecret {
		t.Errorf("Expected body 'secret=%s', got '%s'", expectedSecret, actualBody)
	}
}

func isDockerAvailableForTest() bool {
	if runtime.GOOS == "windows" {
		return false
	}
	cmd := exec.Command("docker", "version")
	return cmd.Run() == nil
}

func getHostIPForTest() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "host.docker.internal"
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}

	return "host.docker.internal"
}
