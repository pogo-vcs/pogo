package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	dockerTypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	imageTypes "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/stdcopy"
)

type sdkClient struct {
	cli *client.Client
}

func newSDKClient(dockerHost string) (Client, error) {
	opts := []client.Opt{
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	}
	if dockerHost != "" {
		opts = append(opts, client.WithHost(dockerHost))
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("create docker sdk client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("ping docker daemon: %w", err)
	}

	return &sdkClient{cli: cli}, nil
}

func (c *sdkClient) PullImage(ctx context.Context, image string) error {
	reader, err := c.cli.ImagePull(ctx, image, imageTypes.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull image %s: %w", image, err)
	}
	defer reader.Close()
	_, _ = io.Copy(io.Discard, reader)
	return nil
}

func (c *sdkClient) BuildImage(ctx context.Context, dockerfilePath string, tag string) error {
	if dockerfilePath == "" {
		return fmt.Errorf("build image: dockerfile path is empty")
	}

	absDockerfile, err := filepath.Abs(dockerfilePath)
	if err != nil {
		return fmt.Errorf("build image: resolve dockerfile path: %w", err)
	}

	contextDir := filepath.Dir(absDockerfile)
	relDockerfile := filepath.Base(absDockerfile)

	tarReader, err := archive.TarWithOptions(contextDir, &archive.TarOptions{})
	if err != nil {
		return fmt.Errorf("build image: archive context: %w", err)
	}
	defer tarReader.Close()

	buildResp, err := c.cli.ImageBuild(
		ctx,
		tarReader,
		dockerTypes.ImageBuildOptions{
			Dockerfile: relDockerfile,
			Tags:       []string{tag},
			Remove:     true,
		},
	)
	if err != nil {
		return fmt.Errorf("build image from %s: %w", dockerfilePath, err)
	}
	defer buildResp.Body.Close()
	_, _ = io.Copy(io.Discard, buildResp.Body)
	return nil
}

func (c *sdkClient) CreateNetwork(ctx context.Context, networkName string) error {
	_, err := c.cli.NetworkCreate(ctx, networkName, network.CreateOptions{})
	if err == nil || errdefs.IsConflict(err) {
		return nil
	}
	return fmt.Errorf("create network %s: %w", networkName, err)
}

func (c *sdkClient) RemoveNetwork(ctx context.Context, networkName string) error {
	if err := c.cli.NetworkRemove(ctx, networkName); err != nil {
		if errdefs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("remove network %s: %w", networkName, err)
	}
	return nil
}

func (c *sdkClient) RunContainer(ctx context.Context, opts RunOptions) error {
	if opts.Image == "" {
		return fmt.Errorf("run container: image is required")
	}

	env := make([]string, 0, len(opts.Environment))
	for key, value := range opts.Environment {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	var binds []string
	for hostPath, containerPath := range opts.Volumes {
		if hostPath == "" || containerPath == "" {
			continue
		}
		binds = append(binds, fmt.Sprintf("%s:%s", hostPath, containerPath))
	}

	containerConfig := &container.Config{
		Image:        opts.Image,
		Env:          env,
		WorkingDir:   opts.WorkingDir,
		Cmd:          opts.Commands,
		AttachStdout: true,
		AttachStderr: true,
	}

	hostConfig := &container.HostConfig{
		Binds:      binds,
		AutoRemove: true,
	}

	networkingConfig := &network.NetworkingConfig{}
	if opts.NetworkName != "" {
		networkingConfig.EndpointsConfig = map[string]*network.EndpointSettings{
			opts.NetworkName: {},
		}
	}

	containerName := opts.Name
	resp, err := c.cli.ContainerCreate(
		ctx,
		containerConfig,
		hostConfig,
		networkingConfig,
		nil,
		containerName,
	)
	if err != nil {
		return fmt.Errorf("run container: create container: %w", err)
	}
	containerID := resp.ID

	if opts.CreateOnly {
		return nil
	}

	stdout := opts.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = io.Discard
	}

	attachCtx, cancelAttach := context.WithCancel(ctx)
	defer cancelAttach()

	attachResp, err := c.cli.ContainerAttach(attachCtx, containerID, container.AttachOptions{
		Stream: true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return fmt.Errorf("run container: attach: %w", err)
	}
	defer attachResp.Close()

	outputErrCh := make(chan error, 1)
	go func() {
		_, copyErr := stdcopy.StdCopy(stdout, stderr, attachResp.Reader)
		if copyErr != nil && !errors.Is(copyErr, io.EOF) && !strings.Contains(copyErr.Error(), "use of closed network connection") {
			outputErrCh <- copyErr
		} else {
			outputErrCh <- nil
		}
	}()

	if err := c.cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("run container: start: %w", err)
	}

	waitCh, errCh := c.cli.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("run container: wait: %w", err)
		}
	case status := <-waitCh:
		if status.Error != nil {
			return fmt.Errorf("run container: %s", status.Error.Message)
		}

		if status.StatusCode != 0 {
			return dockerExitError{statusCode: int(status.StatusCode)}
		}
	}

	if copyErr := <-outputErrCh; copyErr != nil {
		return fmt.Errorf("run container: read output: %w", copyErr)
	}

	return nil
}

func (c *sdkClient) StopContainer(ctx context.Context, containerID string) error {
	timeoutSec := 10
	if err := c.cli.ContainerStop(ctx, containerID, container.StopOptions{
		Timeout: &timeoutSec,
	}); err != nil {
		if errdefs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("stop container %s: %w", containerID, err)
	}
	return nil
}

func (c *sdkClient) RemoveContainer(ctx context.Context, containerID string) error {
	if err := c.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	}); err != nil {
		if errdefs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("remove container %s: %w", containerID, err)
	}
	return nil
}

func (c *sdkClient) CopyToContainer(ctx context.Context, containerID string, srcPath string, dstPath string) error {
	tarReader, err := archive.TarWithOptions(srcPath, &archive.TarOptions{})
	if err != nil {
		return fmt.Errorf("create tar archive from %s: %w", srcPath, err)
	}
	defer tarReader.Close()

	err = c.cli.CopyToContainer(ctx, containerID, dstPath, tarReader, dockerTypes.CopyToContainerOptions{})
	if err != nil {
		return fmt.Errorf("copy %s to container %s:%s: %w", srcPath, containerID, dstPath, err)
	}

	return nil
}

func (c *sdkClient) StartContainer(ctx context.Context, containerID string, stdout io.Writer, stderr io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	attachCtx, cancelAttach := context.WithCancel(ctx)
	defer cancelAttach()

	attachResp, err := c.cli.ContainerAttach(attachCtx, containerID, container.AttachOptions{
		Stream: true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return fmt.Errorf("attach to container: %w", err)
	}
	defer attachResp.Close()

	outputErrCh := make(chan error, 1)
	go func() {
		_, copyErr := stdcopy.StdCopy(stdout, stderr, attachResp.Reader)
		if copyErr != nil && !errors.Is(copyErr, io.EOF) && !strings.Contains(copyErr.Error(), "use of closed network connection") {
			outputErrCh <- copyErr
		} else {
			outputErrCh <- nil
		}
	}()

	if err := c.cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("start container: %w", err)
	}

	waitCh, errCh := c.cli.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("wait for container: %w", err)
		}
	case status := <-waitCh:
		if status.Error != nil {
			return fmt.Errorf("container error: %s", status.Error.Message)
		}

		if status.StatusCode != 0 {
			return dockerExitError{statusCode: int(status.StatusCode)}
		}
	}

	if copyErr := <-outputErrCh; copyErr != nil {
		return fmt.Errorf("read container output: %w", copyErr)
	}

	return nil
}

func (c *sdkClient) Close() error {
	return c.cli.Close()
}

type dockerExitError struct {
	statusCode int
}

func (e dockerExitError) Error() string {
	return fmt.Sprintf("container exited with status %d", e.statusCode)
}

func (e dockerExitError) ExitCode() int {
	return e.statusCode
}