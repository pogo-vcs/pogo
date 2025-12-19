package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/tty"
	"github.com/spf13/cobra"
)

var rmForce bool

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

This command pushes any changes before running.

In interactive mode, you will be prompted to confirm before deletion.
Use --force to skip confirmation.`,
	Example: `# Remove a change and all its descendants
pogo rm experimental-feature-27

# Remove only the specific change, preserving children
pogo rm broken-change-15 --keep-children

# Remove a change that was created by mistake
pogo rm accidental-branch-3

# Skip confirmation prompt
pogo rm experimental-feature-27 --force

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

		_ = c.PushFull(false)

		// Interactive confirmation (unless --force or non-interactive shell)
		if !rmForce && tty.IsInteractive() {
			logData, err := c.GetLogData(0) // 0 means all changes
			if err != nil {
				return errors.Join(errors.New("get log data"), err)
			}

			// Find the target change
			targetChange := logData.FindChangeByPrefix(changeName)
			if targetChange == nil {
				// Change not found in log, let the server handle the error
				if err := c.RemoveChange(changeName, keepChildren); err != nil {
					return errors.Join(errors.New("remove change"), err)
				}
				return nil
			}

			// Collect changes to be deleted
			var changesToDelete []client.LogChangeData
			changesToDelete = append(changesToDelete, *targetChange)

			if !keepChildren {
				descendants := logData.FindDescendants(targetChange.Name)
				changesToDelete = append(changesToDelete, descendants...)
			}

			// Display changes to be deleted
			fmt.Fprintln(cmd.OutOrStdout(), "The following changes will be deleted:")
			for _, change := range changesToDelete {
				desc := "(no description)"
				if change.Description != nil {
					desc = *change.Description
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  - %s: %s\n", change.Name, desc)
			}
			fmt.Fprintln(cmd.OutOrStdout())

			// Ask for confirmation
			var confirmed bool
			confirmMsg := fmt.Sprintf("Are you sure you want to delete %d change(s)?", len(changesToDelete))
			if err := huh.NewConfirm().
				Title(confirmMsg).
				Affirmative("Yes").
				Negative("No").
				Value(&confirmed).
				Run(); err != nil {
				return errors.Join(errors.New("confirmation prompt"), err)
			}

			if !confirmed {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
		}

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
	rmCmd.Flags().BoolVarP(&rmForce, "force", "f", false, "Skip confirmation prompt")
	RootCmd.AddCommand(rmCmd)
}
