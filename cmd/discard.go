package cmd

import (
	"errors"
	"os"

	"github.com/pogo-vcs/pogo/client"
	"github.com/spf13/cobra"
)

var discardCmd = &cobra.Command{
	Use:   "discard",
	Short: "Discard local changes and revert to remote state",
	Long: `Discard all local modifications and revert to the state of the currently 
checked out change as it exists on the remote server.

This is equivalent to running 'pogo edit' with the current change name, 
effectively re-downloading the change from the server and overwriting any 
local modifications.`,
	Example: `# Discard all local changes
pogo discard`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return errors.New("discard takes no arguments")
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

		_, repoId, changeId := c.GetRawData()

		if err := c.Checkout(repoId, changeId); err != nil {
			return errors.Join(errors.New("discard local changes"), err)
		}

		return nil
	},
}

func init() {
	RootCmd.AddCommand(discardCmd)
}