package docker

import (
	"net"
	"strings"
	"time"
)

func getDefaultDockerSocket() string {
	return "npipe:////./pipe/docker_engine"
}

func isSocketAvailable(dockerHost string) bool {
	if strings.HasPrefix(dockerHost, "npipe://") {
		pipePath := strings.TrimPrefix(dockerHost, "npipe://")
		conn, err := net.DialTimeout("unix", pipePath, 1*time.Second)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	}

	if strings.HasPrefix(dockerHost, "unix://") {
		socketPath := strings.TrimPrefix(dockerHost, "unix://")
		conn, err := net.DialTimeout("unix", socketPath, 1*time.Second)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	}

	return false
}