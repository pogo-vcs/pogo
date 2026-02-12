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

var deleteRepoForce bool

var deleteRepoCmd = &cobra.Command{
	Use:   "delete-repo",
	Short: "Delete the current repository from the server",
	Long: `Delete the current repository from the server permanently.

This is a destructive operation that cannot be undone. It will:
- Delete the repository and all its data from the server
- Run garbage collection to free up disk space
- Remove the local .pogo.db file

In interactive mode, you will be prompted to confirm before deletion.
Use --force to skip confirmation.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		wd, err := os.Getwd()
		if err != nil {
			return errors.Join(errors.New("get working directory"), err)
		}
		c, err := client.OpenFromFile(cmd.Context(), wd)
		if err != nil {
			return errors.Join(errors.New("open client"), err)
		}

		// Interactive confirmation (unless --force or non-interactive shell)
		if !deleteRepoForce && tty.IsInteractive() {
			var confirmed bool
			if err := huh.NewConfirm().
				Title("Are you sure you want to delete this repository? This cannot be undone.").
				Affirmative("Yes, delete it").
				Negative("No").
				Value(&confirmed).
				Run(); err != nil {
				c.Close()
				return errors.Join(errors.New("confirmation prompt"), err)
			}

			if !confirmed {
				c.Close()
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
		}

		// Delete from server (DB + GC)
		if err := c.DeleteRepo(); err != nil {
			c.Close()
			return errors.Join(errors.New("delete repository"), err)
		}

		// Get the repo store before closing so we can remove local files
		repoStore := c.GetRepoStore()
		c.Close()

		// Remove local .pogo.db files
		repoStore.Remove()

		fmt.Fprintln(cmd.OutOrStdout(), "Repository deleted successfully.")
		return nil
	},
}

func init() {
	deleteRepoCmd.Flags().BoolVarP(&deleteRepoForce, "force", "f", false, "Skip confirmation prompt")
	RootCmd.AddCommand(deleteRepoCmd)
}
