package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

type Client interface {
	PullImage(ctx context.Context, image string) error
	BuildImage(ctx context.Context, dockerfilePath string, tag string) error
	CreateNetwork(ctx context.Context, networkName string) error
	RemoveNetwork(ctx context.Context, networkName string) error
	RunContainer(ctx context.Context, opts RunOptions) error
	StopContainer(ctx context.Context, containerID string) error
	RemoveContainer(ctx context.Context, containerID string) error
	Close() error
}

type RunOptions struct {
	Image       string
	Name        string
	Commands    []string
	Environment map[string]string
	WorkingDir  string
	NetworkName string
	Volumes     map[string]string
	Stdout      io.Writer
	Stderr      io.Writer
}

func NewClient() (Client, error) {
	dockerHost := os.Getenv("DOCKER_HOST")
	if dockerHost == "" {
		dockerHost = getDefaultDockerSocket()
	}

	if isSocketAvailable(dockerHost) {
		return newSocketClient(dockerHost)
	}

	if isCLIAvailable() {
		return newCLIClient()
	}

	return nil, fmt.Errorf("no Docker socket or CLI available")
}

func isCLIAvailable() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}
