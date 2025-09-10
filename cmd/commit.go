package cmd

import (
	"errors"
	"os"

	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/editor"
	"github.com/pogo-vcs/pogo/tty"
	"github.com/spf13/cobra"
)

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Describe, push, and create a new change in one command",
	Long: `Commit is a convenience command that combines three operations:
1. Set/update the description for the current change (describe)
2. Push all changes to the server (push)
3. Create a new empty change for future work (new)

This command streamlines the common workflow of finishing work on the current
change and starting fresh. It's similar to 'git commit' but remember that in
Pogo, your work is continuously saved to the server rather than being staged
locally first.

The command will:
- Open an editor for the description (unless -m or --no-edit is used)
- Upload all your changes to the server
- Create a new change with the current change as parent
- Switch to the new change automatically
- Display the updated change history

This is ideal when you've completed a logical unit of work and want to start
on something new while preserving the current state.`,
	Example: `# Commit with an editor for the description
pogo commit

# Commit with a description from command line
pogo commit -m "fix: resolve database connection timeout"

# Commit without changing the existing description
pogo commit --no-edit

# Typical workflow
pogo describe -m "feat: add user authentication"
# ... make changes ...
pogo push
# ... make more changes ...
pogo commit  # Finalize and start new change`,
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

		// Display the log
		logOutput, err := c.Log(10, tty.IsInteractive())
		if err != nil {
			return errors.Join(errors.New("fetch log"), err)
		}
		cmd.Print(logOutput)

		return nil
	},
}

func init() {
	commitCmd.Flags().StringP("description", "m", "", "Description for the change")
	commitCmd.Flags().Bool("no-edit", false, "Skip the describe step")
	rootCmd.AddCommand(commitCmd)
}
