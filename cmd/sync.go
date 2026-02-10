package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/tty"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Trunk based update of your divergent line of changes",
	Long:  `Sync is for a Trunk based workflow. It merges your change with a trunk (defaults to main, can be adjusted passing another tag or change name) and if there are no conflicts, sets the given trunk bookmark to the newly created change. If there are conflicts, you are prompted to do so yourself.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var trunk string
		switch len(args) {
		case 0:
			trunk = "main"
		case 1:
			trunk = args[0]
		default:
			return errors.New("invalid number of arguments, please provide at most one change name")
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

		if err := c.PushFull(forcePush); err != nil {
			return errors.Join(errors.New("push full"), err)
		}

		currentChangeInfo, err := c.Info()
		if err != nil {
			return errors.Join(errors.New("get current change name"), err)
		}

		changeId, changeName, err := c.NewChange(nil, []string{trunk, currentChangeInfo.ChangeName})
		if err != nil {
			return errors.Join(errors.New("create new change"), err)
		}

		if err = c.Edit(changeName); err != nil {
			return errors.Join(errors.New("edit revision"), err)
		}

		c.ConfigSetChangeId(changeId)

		// check for conflicts
		newChangeInfo, err := c.Info()
		if err != nil {
			return errors.Join(errors.New("get new change name"), err)
		}
		if newChangeInfo.IsInConflict {
			syncCommand := "pogo sync"
			trunkCliName := trunk
			// if contains cli unsafe characters, quote it
			if strings.ContainsAny(trunk, "/ -*") {
				trunkCliName = fmt.Sprintf("'%s'", trunk)
			}
			if trunk != "main" {
				syncCommand = fmt.Sprintf("pogo sync --trunk %s", trunkCliName)
			}
			return fmt.Errorf("conflicts found, please resolve them and re-sync by running `%s` again", syncCommand)
		}

		// no conflict, set trunk bookmark
		if err := c.SetBookmark(trunk, nil); err != nil {
			return errors.Join(errors.New("set trunk bookmark"), err)
		}

		fmt.Println("Sync successful")

		logOutput, err := c.Log(10, tty.IsInteractive())
		if err != nil {
			return errors.Join(errors.New("fetch log"), err)
		}
		_, _ = fmt.Fprintln(cmd.OutOrStderr(), logOutput)

		return nil
	},
}

func init() {
	RootCmd.AddCommand(syncCmd)
}
