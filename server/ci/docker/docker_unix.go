//go:build !windows

package docker

import (
	"net"
	"strings"
	"time"
)

func getDefaultDockerSocket() string {
	return "unix:///var/run/docker.sock"
}

func isSocketAvailable(dockerHost string) bool {
	if !strings.HasPrefix(dockerHost, "unix://") {
		return false
	}

	socketPath := strings.TrimPrefix(dockerHost, "unix://")

	conn, err := net.DialTimeout("unix", socketPath, 1*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}