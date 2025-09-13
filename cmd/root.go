package cmd

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/pogo-vcs/pogo/client"
	"github.com/spf13/cobra"
)

var Version = "dev"

var (
	globalTimer     bool
	globalTimeStart time.Time
	globalVerbose   bool

	RootCmd = &cobra.Command{
		Use:   "pogo",
		Short: "A centralized version control system that is simple and easy to use",
		Long: `Pogo is a centralized version control system designed to be straightforward and efficient.

Unlike Git, Pogo uses a centralized server as the single source of truth for all your data.
It features an easy-to-use CLI, a simple web UI, and robust support for both text and binary files.
Conflicts are treated as first-class citizens - they can be pushed to the remote and resolved later.

Key concepts:
- Changes: The fundamental unit of work, similar to commits in Git
- Bookmarks: Named references to specific changes (like tags/branches in Git)
- No named branches: Create branches by adding multiple children to a change
- Automatic naming: Changes are automatically named with memorable identifiers

For more information, visit: https://github.com/pogo-vcs/pogo`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if globalTimer {
				globalTimeStart = time.Now()
			}
			if isUnder(cmd, bookmarkCmd, describeCmd, editCmd, logCmd, rmCmd) {
				_ = pushCmd.RunE(cmd, nil)
			}
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if globalTimer {
				_, _ = fmt.Fprintf(cmd.OutOrStderr(), "\nTime: %s\n", time.Since(globalTimeStart))
			}
		},
	}
)

func isUnder(cmd *cobra.Command, targets ...*cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		for _, target := range targets {
			if c == target {
				return true
			}
		}
	}
	return false
}

func init() {
	RootCmd.SilenceUsage = true
	RootCmd.DisableAutoGenTag = true

	// Add global flags
	RootCmd.PersistentFlags().BoolVar(&globalTimer, "time", false, "Measure command execution time")
	RootCmd.PersistentFlags().BoolVarP(&globalVerbose, "verbose", "v", false, "Enable verbose debug logging")
}

func configureClientOutputs(cmd *cobra.Command, c *client.Client) {
	if globalVerbose {
		c.VerboseOut = cmd.OutOrStderr()
	} else {
		c.VerboseOut = io.Discard
	}
}

func Execute() {
	RootCmd.Version = Version
	err := RootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
