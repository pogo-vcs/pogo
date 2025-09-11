package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/tty"
	"github.com/spf13/cobra"
)

var newCmd = &cobra.Command{
	Use:   "new [parent-change-names...]",
	Short: "Create a new change based on one or more parent changes",
	Long: `Create a new change (similar to a commit in Git) based on one or more parent changes.

This command is used when you're ready to start working on something new after
completing your current work. It creates a fresh change that builds upon the
specified parent(s).

Key behaviors:
- If no parents specified, uses the current change as parent
- Automatically switches to the new change after creation
- The previous change becomes read-only to preserve history
- Multiple parents create a merge (combining work from different branches)

Typical workflow:
1. Describe your planned changes with 'pogo describe'
2. Make your code changes
3. Push regularly with 'pogo push' to save your work
4. When done, create a new change with 'pogo new' to start fresh`,
	Example: `# Create a new change from the current change
pogo new

# Create a new change with a description
pogo new -m "feat: implement user profiles"

# Create a new change from a specific parent
pogo new happy-mountain-7

# Create a merge change with multiple parents
pogo new feature-branch-1 feature-branch-2

# Create from a bookmarked change
pogo new main`,
	RunE: func(cmd *cobra.Command, args []string) error {
		description, _ := cmd.Flags().GetString("description")
		var descriptionPtr *string
		if description != "" {
			descriptionPtr = &description
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

		changeId, changeName, err := c.NewChange(descriptionPtr, args)
		if err != nil {
			return errors.Join(errors.New("create new change"), err)
		}

		if err = c.Edit(changeName); err != nil {
			return errors.Join(errors.New("edit revision"), err)
		}

		c.ConfigSetChangeId(changeId)

		// Print the created change name to stdout (primary output)
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), changeName)

		// Display the log to stderr (additional helpful info)
		logOutput, err := c.Log(10, tty.IsInteractive())
		if err != nil {
			return errors.Join(errors.New("fetch log"), err)
		}
		_, _ = fmt.Fprintln(cmd.OutOrStderr(), logOutput)

		return nil
	},
}

func init() {
	newCmd.Flags().StringP("description", "m", "", "Description for the new change")
	rootCmd.AddCommand(newCmd)
}
