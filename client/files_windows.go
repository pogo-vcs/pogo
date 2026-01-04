package client

import (
	"fmt"
	"os"
	"strings"
)

func IsExecutable(absPath string) *bool {
	return nil
}

// CreateSymlink creates a symbolic link on Windows
// Requires Developer Mode (Windows 10+) or Administrator privileges
func CreateSymlink(target, linkPath string) error {
	err := os.Symlink(target, linkPath)
	if err != nil && strings.Contains(err.Error(), "privilege") {
		return fmt.Errorf("symlink creation requires Developer Mode or Administrator privileges on Windows: %w", err)
	}
	return err
}
