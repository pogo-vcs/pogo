package cmd

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/tty"
)

// ShowLogWithPager fetches and displays log output using a scrollable pager if appropriate.
// It handles color detection, fetching the log, and displaying it with or without a pager.
// Parameters:
// - c: the client connection
// - maxChanges: maximum number of changes to display
// - useColor: whether to use colored output (if nil, defaults to tty.IsInteractive())
// - noPager: whether to disable the pager (in addition to global flag)
// - printer: function to print output when not using pager
func ShowLogWithPager(c *client.Client, maxChanges int32, useColor *bool, noPager bool, printer func(string)) error {
	// Determine color setting
	colorEnabled := tty.IsInteractive()
	if useColor != nil {
		colorEnabled = *useColor
	}

	// Fetch log output
	logOutput, err := c.Log(maxChanges, colorEnabled)
	if err != nil {
		return err
	}

	// Display using pager or direct output
	// Check both the local noPager flag and the global flag
	return ShowWithPager(logOutput, noPager || globalNoPager, printer)
}

// ShowWithPager displays content using a scrollable pager if appropriate.
// It automatically determines whether to use the pager based on:
// - Whether the terminal is interactive
// - Whether the content exceeds the terminal height
// - Whether the pager is disabled via noPager parameter
func ShowWithPager(content string, noPager bool, printer func(string)) error {
	// Count lines to determine if we should use the pager
	lineCount := strings.Count(content, "\n")
	shouldUsePager := tty.IsInteractive() && !noPager && lineCount > 20

	if shouldUsePager {
		// Use BubbleTea viewer for scrolling
		m := initialLogModel(content)
		p := tea.NewProgram(m, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			return err
		}
	} else {
		// Direct output using the provided printer function
		printer(content)
	}

	return nil
}
