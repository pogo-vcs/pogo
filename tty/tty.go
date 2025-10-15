package tty

import (
	"github.com/mattn/go-isatty"
	"os"
)

var IsDaemon = false

// IsInteractive returns true if the program is probably running in an interactive terminal
func IsInteractive() bool {
	return !IsDaemon && isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}