package cmd

import (
	"errors"
	"os"

	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/editor"
	"github.com/spf13/cobra"
)

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Describe, push, and create a new change",
	Long:  "Combines describe, push, and new operations into a single command. Sets a description for the current change, pushes it to the server, and creates a new change.",
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

		noEdit, _ := cmd.Flags().GetBool("no-edit")

		if !noEdit {
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
		}

		if err := c.PushFull(false); err != nil {
			return errors.Join(errors.New("push full"), err)
		}

		changeId, revision, err := c.NewChange(nil, []string{})
		if err != nil {
			return errors.Join(errors.New("create new change"), err)
		}

		if err = c.Edit(revision); err != nil {
			return errors.Join(errors.New("edit revision"), err)
		}

		c.ConfigSetChangeId(changeId)

		// Display the log using pager
		if err := ShowLogWithPager(c, 10, nil, false, func(s string) {
			cmd.Println(s)
		}); err != nil {
			return errors.Join(errors.New("display log"), err)
		}

		return nil
	},
}

func init() {
	commitCmd.Flags().StringP("description", "m", "", "Description for the change")
	commitCmd.Flags().Bool("no-edit", false, "Skip the describe step")
	rootCmd.AddCommand(commitCmd)
}
