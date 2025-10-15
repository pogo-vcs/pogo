package ci

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestExecutor_ContainerTask(t *testing.T) {
	if !isDockerAvailable() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()

	executor := NewExecutor()

	configYAML := `
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
        - "echo 'Container task executed successfully'"
`

	configFiles := map[string][]byte{
		"ci.yaml": []byte(configYAML),
	}

	event := Event{
		Rev:         "main",
		ArchiveUrl:  "https://example.com/archive",
		Author:      "testuser",
		Description: "Container test",
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	results, err := executor.ExecuteForBookmarkEvent(ctx, configFiles, event, EventTypePush)
	if err != nil {
		t.Errorf("ExecuteForBookmarkEvent() error = %v", err)
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
}

func TestExecutor_ContainerTaskWithServices(t *testing.T) {
	if !isDockerAvailable() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()

	executor := NewExecutor()

	configYAML := `
version: 1
on:
  push:
    bookmarks: ["main"]
do:
  - type: container
    container:
      image: alpine:latest
      services:
        - name: nginx
          image: nginx:alpine
      commands:
        - sh
        - -c
        - "apk add --no-cache curl && curl -f http://nginx:80"
`

	configFiles := map[string][]byte{
		"ci.yaml": []byte(configYAML),
	}

	event := Event{
		Rev:         "main",
		ArchiveUrl:  "https://example.com/archive",
		Author:      "testuser",
		Description: "Container test",
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	results, err := executor.ExecuteForBookmarkEvent(ctx, configFiles, event, EventTypePush)
	if err != nil {
		t.Errorf("ExecuteForBookmarkEvent() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Success {
		t.Fatal("expected container task with services to succeed")
	}
	if results[0].StatusCode != 0 {
		t.Fatalf("expected container exit code 0, got %d", results[0].StatusCode)
	}
}

func TestExecutor_ContainerTaskWithWorkspaceMount(t *testing.T) {
	if !isDockerAvailable() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()

	tmpDir := t.TempDir()
	testFile := "test.txt"
	testContent := "hello from workspace"
	err := os.WriteFile(tmpDir+"/"+testFile, []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	origPath := os.Getenv("PATH")
	defer os.Setenv("PATH", origPath)
	os.Setenv("PATH", "/nonexistent")

	executor := NewExecutor()
	executor.SetRepoContentDir(tmpDir)

	configYAML := `
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
        - "ls -la /workspace && cat /workspace/test.txt"
`

	configFiles := map[string][]byte{
		"ci.yaml": []byte(configYAML),
	}

	event := Event{
		Rev:         "main",
		ArchiveUrl:  "https://example.com/archive",
		Author:      "testuser",
		Description: "Container test",
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	results, err := executor.ExecuteForBookmarkEvent(ctx, configFiles, event, EventTypePush)
	if err != nil {
		t.Errorf("ExecuteForBookmarkEvent() error = %v\nLog: %s", err, results[0].Log)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Success {
		t.Fatalf("expected container task to succeed\nLog: %s", results[0].Log)
	}
	if results[0].StatusCode != 0 {
		t.Fatalf("expected container exit code 0, got %d\nLog: %s", results[0].StatusCode, results[0].Log)
	}
}

func isDockerAvailable() bool {
	if runtime.GOOS == "windows" && os.Getenv("CI") != "" {
		// GitHub Windows runners don't have WSL configured for Docker
		return false
	}
	// check for docker socket
	if dockerSocketExists() {
		return true
	}
	cmd := exec.Command("docker", "version")
	return cmd.Run() == nil
}