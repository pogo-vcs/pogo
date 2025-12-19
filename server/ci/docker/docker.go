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
	CopyToContainer(ctx context.Context, containerID string, srcPath string, dstPath string) error
	StartContainer(ctx context.Context, containerID string, stdout io.Writer, stderr io.Writer) error
	StopContainer(ctx context.Context, containerID string) error
	RemoveContainer(ctx context.Context, containerID string) error
	ExecInContainer(ctx context.Context, containerID string, command string, stdout io.Writer, stderr io.Writer) error
	Close() error
}

type RunOptions struct {
	Image       string
	Name        string
	Commands    []string
	Entrypoint  []string
	Environment map[string]string
	WorkingDir  string
	NetworkName string
	Volumes     map[string]string
	Stdout      io.Writer
	Stderr      io.Writer
	CreateOnly  bool
}

func NewClient() (Client, error) {
	dockerHost := os.Getenv("DOCKER_HOST")
	if dockerHost == "" {
		dockerHost = getDefaultDockerSocket()
	}

	sdkClient, err := newSDKClient(dockerHost)
	if err == nil {
		return sdkClient, nil
	}
	sdkErr := err

	if isCLIAvailable() {
		if dockerHost != "" {
			return newSocketClient(dockerHost)
		}
		return newCLIClient()
	}

	return nil, fmt.Errorf("docker sdk unavailable: %w; docker CLI not found", sdkErr)
}

func isCLIAvailable() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}
