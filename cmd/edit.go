package cmd

import (
	"errors"
	"os"

	"github.com/pogo-vcs/pogo/client"
	"github.com/spf13/cobra"
)

var editCmd = &cobra.Command{
	Aliases: []string{"checkout"},
	Use:     "edit [change name]",
	Short:   "Sets the specified revision as the working-copy revision",
	Long: `Downloads all files from the specified revision and replaces the working-copy files with it.
Ignored files are excluded.`,

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
