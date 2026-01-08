//go:build windows

package client

import "os"

func getInode(info os.FileInfo) int64 {
	// Windows doesn't have reliable inodes
	// Could use GetFileInformationByHandle for file index, but simpler to use 0
	return 0
}
