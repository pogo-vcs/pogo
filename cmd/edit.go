package cmd

import (
	"errors"
	"os"

	"github.com/pogo-vcs/pogo/client"
	"github.com/spf13/cobra"
)

var editCmd = &cobra.Command{
	Aliases: []string{"checkout"},
	Use:     "edit <change-name>",
	Short:   "Switch to a different change for editing",
	Long: `Switch your working directory to a different change for editing.

This command downloads all files from the specified change and replaces your
current working directory contents with them. It's similar to 'git checkout'
but in Pogo's centralized model.

Important notes:
- Any uncommitted local changes will be LOST - push first if you want to keep them
- The change you switch to becomes your new working change
- You can edit any change, even if it has children (though pushing will require --force)
- Files in .pogoignore are not affected by this operation

Use this command to:
- Switch between different lines of development
- Go back to an earlier version to fix a bug
- Review someone else's changes
- Start working from a different base`,
	Example: `# Switch to change ab23
pogo edit ab23

# Switch to the change marked as "main"
pogo edit main

# Switch to a tagged release
pogo edit v1.0.0

# Use the checkout alias (familiar to Git users)
pogo checkout feature-branch`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return errors.New("revision name is required")
		}

		revision := args[0]

		wd, err := os.Getwd()
		if err != nil {
			return errors.Join(errors.New("get working directory"), err)
		}
		c, err := client.OpenFromFile(cmd.Context(), wd)
		if err != nil {
			return errors.Join(errors.New("open client"), err)
		}
		defer c.Close()

		if err := c.Edit(revision); err != nil {
			return errors.Join(errors.New("edit revision"), err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(editCmd)
}
