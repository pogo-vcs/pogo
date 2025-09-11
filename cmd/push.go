package cmd

import (
	"errors"
	"os"

	"github.com/pogo-vcs/pogo/client"
	"github.com/spf13/cobra"
)

var forcePush bool

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push all changes to the configured Pogo server",
	Long: `Push all local changes to the configured Pogo server.

This command uploads all modified, added, and deleted files to the server,
updating the current change. Unlike Git, you don't need to stage files first,
all changes in your working directory are pushed.

The push might be rejected if:
- The current change is read-only (has children or bookmarks pointing to it)
- You don't have write permissions to the repository
- Network or server issues occur

Use the --force flag to override read-only protection, but be careful as this
can break the history for other users.`,
	Example: `# Push all changes to the server
pogo push

# Force push even if the change is read-only (use with caution!)
pogo push --force

# Short form of force push
pogo push -f`,
	RunE: func(cmd *cobra.Command, args []string) error {
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

		if err := c.PushFull(forcePush); err != nil {
			return errors.Join(errors.New("push full"), err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(pushCmd)
	pushCmd.Flags().BoolVarP(&forcePush, "force", "f", false, "Force push even if the change is readonly")
}
