package docker

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type socketClient struct {
	dockerHost string
}

func newSocketClient(dockerHost string) (Client, error) {
	return &socketClient{dockerHost: dockerHost}, nil
}

func (c *socketClient) PullImage(ctx context.Context, image string) error {
	cmd := exec.CommandContext(ctx, "docker", "pull", image)
	cmd.Env = append(cmd.Env, fmt.Sprintf("DOCKER_HOST=%s", c.dockerHost))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pull image %s: %w: %s", image, err, string(output))
	}
	return nil
}

func (c *socketClient) BuildImage(ctx context.Context, dockerfilePath string, tag string) error {
	cmd := exec.CommandContext(ctx, "docker", "build", "-f", dockerfilePath, "-t", tag, ".")
	cmd.Env = append(cmd.Env, fmt.Sprintf("DOCKER_HOST=%s", c.dockerHost))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build image from %s: %w: %s", dockerfilePath, err, string(output))
	}
	return nil
}

func (c *socketClient) CreateNetwork(ctx context.Context, networkName string) error {
	cmd := exec.CommandContext(ctx, "docker", "network", "create", networkName)
	cmd.Env = append(cmd.Env, fmt.Sprintf("DOCKER_HOST=%s", c.dockerHost))
	output, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(string(output), "already exists") {
		return fmt.Errorf("create network %s: %w: %s", networkName, err, string(output))
	}
	return nil
}

func (c *socketClient) RemoveNetwork(ctx context.Context, networkName string) error {
	cmd := exec.CommandContext(ctx, "docker", "network", "rm", networkName)
	cmd.Env = append(cmd.Env, fmt.Sprintf("DOCKER_HOST=%s", c.dockerHost))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove network %s: %w: %s", networkName, err, string(output))
	}
	return nil
}

func (c *socketClient) RunContainer(ctx context.Context, opts RunOptions) error {
	if opts.CreateOnly {
		args := []string{"create"}

		if opts.Name != "" {
			args = append(args, "--name", opts.Name)
		}

		if opts.NetworkName != "" {
			args = append(args, "--network", opts.NetworkName)
		}

		if opts.WorkingDir != "" {
			args = append(args, "--workdir", opts.WorkingDir)
		}

		for key, value := range opts.Environment {
			args = append(args, "-e", fmt.Sprintf("%s=%s", key, value))
		}

		for host, container := range opts.Volumes {
			args = append(args, "-v", fmt.Sprintf("%s:%s", host, container))
		}

		if len(opts.Entrypoint) > 0 {
			args = append(args, "--entrypoint", opts.Entrypoint[0])
		}

		args = append(args, opts.Image)
		if len(opts.Entrypoint) > 1 {
			args = append(args, opts.Entrypoint[1:]...)
		}
		args = append(args, opts.Commands...)

		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.Env = append(cmd.Env, fmt.Sprintf("DOCKER_HOST=%s", c.dockerHost))
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("create container: %w: %s", err, string(output))
		}
		return nil
	}

	args := []string{"run", "--rm"}

	if opts.Name != "" {
		args = append(args, "--name", opts.Name)
	}

	if opts.NetworkName != "" {
		args = append(args, "--network", opts.NetworkName)
	}

	if opts.WorkingDir != "" {
		args = append(args, "--workdir", opts.WorkingDir)
	}

	for key, value := range opts.Environment {
		args = append(args, "-e", fmt.Sprintf("%s=%s", key, value))
	}

	for host, container := range opts.Volumes {
		args = append(args, "-v", fmt.Sprintf("%s:%s", host, container))
	}

	if len(opts.Entrypoint) > 0 {
		args = append(args, "--entrypoint", opts.Entrypoint[0])
	}

	args = append(args, opts.Image)
	if len(opts.Entrypoint) > 1 {
		args = append(args, opts.Entrypoint[1:]...)
	}
	args = append(args, opts.Commands...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = append(cmd.Env, fmt.Sprintf("DOCKER_HOST=%s", c.dockerHost))
	if opts.Stdout != nil {
		cmd.Stdout = opts.Stdout
	}
	if opts.Stderr != nil {
		cmd.Stderr = opts.Stderr
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run container: %w", err)
	}

	return nil
}

func (c *socketClient) StopContainer(ctx context.Context, containerID string) error {
	cmd := exec.CommandContext(ctx, "docker", "stop", containerID)
	cmd.Env = append(cmd.Env, fmt.Sprintf("DOCKER_HOST=%s", c.dockerHost))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("stop container %s: %w: %s", containerID, err, string(output))
	}
	return nil
}

func (c *socketClient) RemoveContainer(ctx context.Context, containerID string) error {
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", containerID)
	cmd.Env = append(cmd.Env, fmt.Sprintf("DOCKER_HOST=%s", c.dockerHost))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove container %s: %w: %s", containerID, err, string(output))
	}
	return nil
}

func (c *socketClient) CopyToContainer(ctx context.Context, containerID string, srcPath string, dstPath string) error {
	cmd := exec.CommandContext(ctx, "docker", "cp", srcPath+"/.", containerID+":"+dstPath)
	cmd.Env = append(cmd.Env, fmt.Sprintf("DOCKER_HOST=%s", c.dockerHost))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("copy %s to container %s:%s: %w: %s", srcPath, containerID, dstPath, err, string(output))
	}
	return nil
}

func (c *socketClient) StartContainer(ctx context.Context, containerID string, stdout io.Writer, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "docker", "start", "-a", containerID)
	cmd.Env = append(cmd.Env, fmt.Sprintf("DOCKER_HOST=%s", c.dockerHost))
	if stdout != nil {
		cmd.Stdout = stdout
	}
	if stderr != nil {
		cmd.Stderr = stderr
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("start container: %w", err)
	}

	return nil
}

func (c *socketClient) ExecInContainer(ctx context.Context, containerID string, command string, stdout io.Writer, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "docker", "exec", containerID, "sh", "-c", command)
	cmd.Env = append(cmd.Env, fmt.Sprintf("DOCKER_HOST=%s", c.dockerHost))
	if stdout != nil {
		cmd.Stdout = stdout
	}
	if stderr != nil {
		cmd.Stderr = stderr
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("exec in container: %w", err)
	}

	return nil
}

func (c *socketClient) Close() error {
	return nil
}
