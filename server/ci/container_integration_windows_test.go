//go:build windows

package ci

import (
	"time"

	"github.com/Microsoft/go-winio"
)

func dockerSocketExists() bool {
	pipe := `\\.\pipe\docker_engine`
	timeout := time.Duration(2 * time.Second)
	conn, err := winio.DialPipe(pipe, &timeout) // tries to connect -> indicates daemon listening
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}