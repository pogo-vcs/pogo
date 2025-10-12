//go:build !windows

package ci

import "os"

func dockerSocketExists() bool {
	const sock = "/var/run/docker.sock"
	_, err := os.Stat(sock)
	return err == nil
}
