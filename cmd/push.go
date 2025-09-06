package cmd

import (
	"errors"
	"os"

	"github.com/pogo-vcs/pogo/client"
	"github.com/spf13/cobra"
)

var forcePush bool

var pushCmd = &cobra.Command{
	Use:    "push",
	Short:  "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
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
