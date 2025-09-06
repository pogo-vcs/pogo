package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var Version = "dev"

var (
	globalNoPager   bool
	globalTimer     bool
	globalTimeStart time.Time

	rootCmd = &cobra.Command{
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
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if globalTimer {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nTime: %s\n", time.Since(globalTimeStart))
			}
		},
	}
)

func init() {
	rootCmd.SilenceUsage = true
	rootCmd.DisableAutoGenTag = true

	// Add global flags
	rootCmd.PersistentFlags().BoolVar(&globalNoPager, "no-pager", false, "Disable pager for all output")
	rootCmd.PersistentFlags().BoolVar(&globalTimer, "time", false, "Measure command execution time")
}

func Execute() {
	rootCmd.Version = Version
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
