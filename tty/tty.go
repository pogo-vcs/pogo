package tty

import (
	"github.com/mattn/go-isatty"
	"os"
)

// IsInteractive returns true if the program is probably running in an interactive terminal
func IsInteractive() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}
