package cmd

import (
	"errors"
	"os"

	"github.com/pogo-vcs/pogo/client"
	"github.com/spf13/cobra"
)

var newCmd = &cobra.Command{
	Use:   "new [parent-change-names...]",
	Short: "Create a new change based on another",
	Long: `Create a new change based on one or more parent changes.
If no parent change names are provided, the current checked out change will be used as the parent.
Multiple parents can be specified for merge operations (not yet implemented).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		description, _ := cmd.Flags().GetString("description")
		var descriptionPtr *string
		if description != "" {
			descriptionPtr = &description
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

		changeId, revision, err := c.NewChange(descriptionPtr, args)
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
	newCmd.Flags().StringP("description", "m", "", "Description for the new change")
	rootCmd.AddCommand(newCmd)
}
