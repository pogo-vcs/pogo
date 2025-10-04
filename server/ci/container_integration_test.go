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
		Rev:        "main",
		ArchiveUrl: "https://example.com/archive",
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	err := executor.ExecuteForBookmarkEvent(ctx, configFiles, event, EventTypePush)
	if err != nil {
		t.Errorf("ExecuteForBookmarkEvent() error = %v", err)
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
		Rev:        "main",
		ArchiveUrl: "https://example.com/archive",
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	err := executor.ExecuteForBookmarkEvent(ctx, configFiles, event, EventTypePush)
	if err != nil {
		t.Errorf("ExecuteForBookmarkEvent() error = %v", err)
	}
}

func isDockerAvailable() bool {
	if runtime.GOOS == "windows" && os.Getenv("CI") != "" {
		// GitHub Windows runners don't have WSL configured for Docker
		return false
	}
	cmd := exec.Command("docker", "version")
	return cmd.Run() == nil
}
