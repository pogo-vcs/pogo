package docker

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type cliClient struct{}

func newCLIClient() (Client, error) {
	return &cliClient{}, nil
}

func (c *cliClient) PullImage(ctx context.Context, image string) error {
	cmd := exec.CommandContext(ctx, "docker", "pull", image)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pull image %s: %w: %s", image, err, string(output))
	}
	return nil
}

func (c *cliClient) BuildImage(ctx context.Context, dockerfilePath string, tag string) error {
	cmd := exec.CommandContext(ctx, "docker", "build", "-f", dockerfilePath, "-t", tag, ".")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build image from %s: %w: %s", dockerfilePath, err, string(output))
	}
	return nil
}

func (c *cliClient) CreateNetwork(ctx context.Context, networkName string) error {
	cmd := exec.CommandContext(ctx, "docker", "network", "create", networkName)
	output, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(string(output), "already exists") {
		return fmt.Errorf("create network %s: %w: %s", networkName, err, string(output))
	}
	return nil
}

func (c *cliClient) RemoveNetwork(ctx context.Context, networkName string) error {
	cmd := exec.CommandContext(ctx, "docker", "network", "rm", networkName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove network %s: %w: %s", networkName, err, string(output))
	}
	return nil
}

func (c *cliClient) RunContainer(ctx context.Context, opts RunOptions) error {
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

func (c *cliClient) StopContainer(ctx context.Context, containerID string) error {
	cmd := exec.CommandContext(ctx, "docker", "stop", containerID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("stop container %s: %w: %s", containerID, err, string(output))
	}
	return nil
}

func (c *cliClient) RemoveContainer(ctx context.Context, containerID string) error {
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", containerID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove container %s: %w: %s", containerID, err, string(output))
	}
	return nil
}

func (c *cliClient) CopyToContainer(ctx context.Context, containerID string, srcPath string, dstPath string) error {
	cmd := exec.CommandContext(ctx, "docker", "cp", srcPath+"/.", containerID+":"+dstPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("copy %s to container %s:%s: %w: %s", srcPath, containerID, dstPath, err, string(output))
	}
	return nil
}

func (c *cliClient) StartContainer(ctx context.Context, containerID string, stdout io.Writer, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "docker", "start", "-a", containerID)
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

func (c *cliClient) ExecInContainer(ctx context.Context, containerID string, command string, stdout io.Writer, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "docker", "exec", containerID, "sh", "-c", command)
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

func (c *cliClient) Close() error {
	return nil
}
