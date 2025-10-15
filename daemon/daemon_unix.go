//go:build !windows

package daemon

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

func getHomeDir() (string, error) {
	u, err := user.Current()
	if err == nil {
		return u.HomeDir, nil
	}

	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		return "", errors.New("user home directory not found")
	}
	return homeDir, nil
}

func getUID() (string, error) {
	u, err := user.Current()
	if err == nil {
		return u.Uid, nil
	}

	cmd := exec.Command("id", "-u")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}