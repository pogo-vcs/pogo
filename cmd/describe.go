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
		Short: "Set the description for the current change",
		Long: `Set or modify the description for the current change.

In Pogo's workflow, you should describe your changes BEFORE you start working.
This helps you think through what you're about to do and communicate your
intentions to others. You can update the description as you work to reflect
any changes in your implementation approach.

If no description is provided via the -m flag, an editor will open where you
can write a detailed description. The description follows the Conventional
Commits format by default.

The description is crucial for understanding the history of your project and
should explain both WHAT changed and WHY it changed.

This command pushes any changes before running.`,
		Example: `# Open an editor to write/edit the description
pogo describe

# Set description directly from command line
pogo describe -m "feat: add user authentication system"

# Use aliases for shorter commands
pogo desc -m "fix: resolve memory leak in data processor"
pogo rephrase -m "docs: update API documentation"`,
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
