package cmd

import (
	"errors"
	"os"

	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/editor"
	"github.com/spf13/cobra"
)

var (
	describeCmd = &cobra.Command{
		Use: "describe",
		Aliases: []string{
			"desc",
			"rephrase",
		},
		Short: "Set the change description",
		Long:  "Display the change history as parent/child relations",
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

			var newDescription string
			if cmd.Flags().Changed("description") {
				newDescription, _ = cmd.Flags().GetString("description")
			} else {
				oldDescription, err := c.GetDescription()
				if err != nil {
					return errors.Join(errors.New("get description"), err)
				}
				if oldDescription == nil {
					newDescription, err = editor.ConventionalCommit("")
				} else {
					newDescription, err = editor.ConventionalCommit(*oldDescription)
				}
				if err != nil {
					return errors.Join(errors.New("edit description"), err)
				}
			}

			if newDescription == "" {
				return errors.New("description cannot be empty")
			}

			err = c.SetDescription(newDescription)
			if err != nil {
				return errors.Join(errors.New("set description"), err)
			}

			return nil
		},
	}
)

func init() {
	describeCmd.Flags().StringP("description", "m", "", "Description for the change")
	rootCmd.AddCommand(describeCmd)
}
