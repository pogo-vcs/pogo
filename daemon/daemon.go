package daemon

import (
	"errors"
	"os"
	"os/exec"
)

func getSelfPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		if len(os.Args) > 0 {
			return os.Args[0], nil
		}
		return "", err
	}
	return exe, nil
}

func sysRun(name string, arg ...string) error {
	cmd := exec.Command(name, arg...)
	return cmd.Run()
}

var ErrAlreadyRunning = errors.New("daemon already running")