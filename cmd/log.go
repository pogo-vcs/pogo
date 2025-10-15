package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/tty"
	"github.com/spf13/cobra"
)

var (
	logColorFlag  bool
	logNumberFlag int32
	logJSONFlag   bool
	logCmd        = &cobra.Command{
		Use:   "log",
		Short: "Show the change history",
		Long: `Display the change history of the repository as a tree of parent/child relationships.

The log shows:
- Change names (automatically generated memorable identifiers)
- Descriptions of what changed and why
- Parent/child relationships between changes
- Bookmarks pointing to specific changes
- The currently checked-out change (marked with *)

Unlike Git's linear log, Pogo's log shows the true tree structure of your
repository, making it easy to see branches and merges. Changes are shown
from newest to oldest by default.`,
		Example: `# Show the last 10 changes (default)
pogo log

# Show the last 50 changes
pogo log -n 50

# Disable colored output
pogo log --color=false

# Output as JSON
pogo log --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return errors.New("too many arguments")
			}

			wd, err := os.Getwd()
			if err != nil {
				return errors.Join(errors.New("get working directory"), err)
			}
			c, err := client.OpenFromFile(cmd.Context(), wd)
			if err != nil {
				return errors.Join(errors.New("open client"), err)
			}
			defer c.Close()
			configureClientOutputs(cmd, c)

			// Fetch and display the log output
			var logOutput string

			if logJSONFlag {
				logOutput, err = c.LogJSON(logNumberFlag)
			} else {
				logOutput, err = c.Log(logNumberFlag, logColorFlag)
			}

			if err != nil {
				return errors.Join(errors.New("fetch log"), err)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), logOutput)

			return nil
		},
	}
)

func init() {
	logCmd.Flags().BoolVar(&logColorFlag, "color", tty.IsInteractive(), "Enable colored output")
	logCmd.Flags().Int32VarP(&logNumberFlag, "number", "n", 10, "Maximum number of changes to display")
	logCmd.Flags().BoolVar(&logJSONFlag, "json", false, "Output log data as JSON")
	RootCmd.AddCommand(logCmd)
}