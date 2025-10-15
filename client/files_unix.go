//go:build !windows
// +build !windows

package client

import (
	"os"

	"github.com/pogo-vcs/pogo/ptr"
)

func IsExecutable(absPath string) *bool {
	f, err := os.Stat(absPath)
	if err != nil {
		return nil
	}
	return ptr.Bool(f.Mode().Perm()&0111 != 0)
}