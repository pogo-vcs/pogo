package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/pogo-vcs/pogo/client"
	"github.com/spf13/cobra"
)

var rmCmd = &cobra.Command{
	Use:   "rm <change-name>",
	Short: "Remove a change from the repository",
	Long: `Remove a change from the repository permanently.

This is a destructive operation that cannot be undone. Use with caution!

By default, this command removes:
- The specified change
- All child changes (recursively)
- All associated file data

With --keep-children flag:
- Only removes the specified change
- Reconnects children to the removed change's parent(s)
- Preserves the rest of the history tree

You cannot remove:
- The change you're currently working on
- Changes that have bookmarks pointing to them
- The root change of the repository

This command is useful for:
- Cleaning up experimental branches that didn't work out
- Removing accidentally created changes
- Pruning unnecessary history before archiving

This command pushes any changes before running.`,
	Example: `# Remove a change and all its descendants
pogo rm experimental-feature-27

# Remove only the specific change, preserving children
pogo rm broken-change-15 --keep-children

# Remove a change that was created by mistake
pogo rm accidental-branch-3

# Cannot remove bookmarked changes
pogo rm main  # Error: cannot remove bookmarked change`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		keepChildren, _ := cmd.Flags().GetBool("keep-children")
		changeName := args[0]

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

		if err := c.RemoveChange(changeName, keepChildren); err != nil {
			return errors.Join(errors.New("remove change"), err)
		}

		if keepChildren {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed change: %s (children preserved)\n", changeName)
		} else {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed change: %s (including all children)\n", changeName)
		}

		return nil
	},
}

func init() {
	rmCmd.Flags().Bool("keep-children", false, "Only remove the specified change and move its children to its parents")
	rootCmd.AddCommand(rmCmd)
}
