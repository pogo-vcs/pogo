package ci

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestExecutor_ContainerTaskWithHTTPServer(t *testing.T) {
	if !isDockerAvailable() {
		t.Skip("Docker not available")
	}

	ctx := context.Background()

	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello from test server"))
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	defer server.Close()

	executor := NewExecutor()

	hostIP := getHostIP()
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
        - "apk add --no-cache curl && curl -f %s"
`, testURL)

	configFiles := map[string][]byte{
		"ci.yaml": []byte(configYAML),
	}

	event := Event{
		Rev:        "main",
		ArchiveUrl: "https://example.com/archive",
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

func getHostIP() string {
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
