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
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExecutor_WebhookWithSecrets(t *testing.T) {
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
  - webhook:
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

func TestExecutor_WebhookWithMissingSecretFails(t *testing.T) {
	ctx := context.Background()

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
  - webhook:
      url: %s/webhook
      method: POST
      body: token={{ secret "MISSING_SECRET_XYZ_TEST" }}
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

	_, err := executor.ExecuteForBookmarkEvent(ctx, configFiles, event, EventTypePush)
	if err == nil {
		t.Fatal("expected error when secret is missing, got nil")
	}

	expectedErrMsg := `secret "MISSING_SECRET_XYZ_TEST" not found`
	if !strings.Contains(err.Error(), expectedErrMsg) {
		t.Errorf("expected error to contain %q, got %q", expectedErrMsg, err.Error())
	}
}

func TestExecutor_WebhookWithSecretFromEnvVar(t *testing.T) {
	ctx := context.Background()

	expectedSecret := "env-var-secret-value"
	envKey := "POGO_TEST_SECRET_FROM_ENV"

	// Set environment variable
	t.Setenv(envKey, expectedSecret)

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
	// Empty secrets map - should fall back to env var
	executor.SetSecrets(map[string]string{})

	configYAML := fmt.Sprintf(`
version: 1
on:
  push:
    bookmarks: ["main"]
do:
  - webhook:
      url: %s/webhook
      method: POST
      body: token={{ secret "%s" }}
`, testServer.URL, envKey)

	configFiles := map[string][]byte{
		"ci.yaml": []byte(configYAML),
	}

	event := Event{
		Rev:         "main",
		ArchiveUrl:  "https://example.com/archive",
		Author:      "testuser",
		Description: "Secret from env test",
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

	mu.Lock()
	actualBody := receivedBody
	mu.Unlock()

	if actualBody != "token="+expectedSecret {
		t.Errorf("Expected body 'token=%s', got '%s'", expectedSecret, actualBody)
	}
}

func TestExecutor_SecretMapTakesPriorityOverEnvVar(t *testing.T) {
	ctx := context.Background()

	secretMapValue := "from-secrets-map"
	envVarValue := "from-env-var"
	secretKey := "POGO_TEST_PRIORITY_SECRET"

	// Set environment variable
	t.Setenv(secretKey, envVarValue)

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
	// Secrets map has the key - should take priority over env var
	executor.SetSecrets(map[string]string{
		secretKey: secretMapValue,
	})

	configYAML := fmt.Sprintf(`
version: 1
on:
  push:
    bookmarks: ["main"]
do:
  - webhook:
      url: %s/webhook
      method: POST
      body: token={{ secret "%s" }}
`, testServer.URL, secretKey)

	configFiles := map[string][]byte{
		"ci.yaml": []byte(configYAML),
	}

	event := Event{
		Rev:         "main",
		ArchiveUrl:  "https://example.com/archive",
		Author:      "testuser",
		Description: "Secret priority test",
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

	mu.Lock()
	actualBody := receivedBody
	mu.Unlock()

	// Should use the value from secrets map, not env var
	if actualBody != "token="+secretMapValue {
		t.Errorf("Expected body 'token=%s' (from secrets map), got '%s'", secretMapValue, actualBody)
	}
}

func TestExecutor_ContainerWithSecrets(t *testing.T) {
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
  - container:
      image: alpine:latest
      commands:
        - apk add --no-cache curl
        - curl -X POST -d 'secret={{ secret "API_KEY" }}' %s/callback
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

func TestExecutor_ContainerWithSecretsInEnvironment(t *testing.T) {
	if !isDockerAvailableForTest() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()

	expectedSecret := "env-secret-value-abc"
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
		"MY_SECRET_TOKEN": expectedSecret,
	})

	hostIP := getHostIPForTest()
	testURL := fmt.Sprintf("http://%s:%d", hostIP, port)

	// Test using secret in environment variable - this is the user's use case
	configYAML := fmt.Sprintf(`
version: 1
on:
  push:
    bookmarks: ["main"]
do:
  - container:
      image: alpine:latest
      environment:
        SECRET_TOKEN: '{{ secret "MY_SECRET_TOKEN" }}'
      commands:
        - apk add --no-cache curl
        - curl -X POST -d "token=$SECRET_TOKEN" %s/callback
`, testURL)

	configFiles := map[string][]byte{
		"ci.yaml": []byte(configYAML),
	}

	event := Event{
		Rev:         "main",
		ArchiveUrl:  "https://example.com/archive",
		Author:      "testuser",
		Description: "Secret in env test",
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
		t.Fatalf("expected container task to succeed, log: %s", results[0].Log)
	}
	if results[0].StatusCode != 0 {
		t.Fatalf("expected container exit code 0, got %d", results[0].StatusCode)
	}

	mu.Lock()
	actualBody := receivedBody
	mu.Unlock()

	if actualBody != "token="+expectedSecret {
		t.Errorf("Expected body 'token=%s', got '%s'", expectedSecret, actualBody)
	}
}

func TestExecutor_ContainerWithMissingSecretInEnvironmentFails(t *testing.T) {
	ctx := context.Background()

	executor := NewExecutor()
	executor.SetSecrets(map[string]string{})

	// Use a unique secret name that won't exist in any environment
	secretName := "POGO_TEST_NONEXISTENT_SECRET_ABC123"

	// Test using missing secret in environment variable
	configYAML := fmt.Sprintf(`
version: 1
on:
  push:
    bookmarks: ["main"]
do:
  - container:
      image: alpine:latest
      environment:
        MY_TOKEN: '{{ secret "%s" }}'
      commands:
        - echo "test"
`, secretName)

	configFiles := map[string][]byte{
		"ci.yaml": []byte(configYAML),
	}

	event := Event{
		Rev:         "main",
		ArchiveUrl:  "https://example.com/archive",
		Author:      "testuser",
		Description: "Missing secret in env test",
	}

	_, err := executor.ExecuteForBookmarkEvent(ctx, configFiles, event, EventTypePush)
	if err == nil {
		t.Fatal("expected error when secret is missing in environment, got nil")
	}

	expectedErrMsg := fmt.Sprintf(`secret %q not found`, secretName)
	if !strings.Contains(err.Error(), expectedErrMsg) {
		t.Errorf("expected error to contain %q, got %q", expectedErrMsg, err.Error())
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
