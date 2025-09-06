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
	Short: "Remove a change",
	Long: `Remove a change from the repository.
By default, all children of the change are also removed recursively.
Use --keep-children to only remove the specified change and move its children to its parents.`,
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
