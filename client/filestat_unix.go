//go:build !windows

package client

import (
	"os"
	"syscall"
)

func getInode(info os.FileInfo) int64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return int64(stat.Ino)
	}
	return 0
}
