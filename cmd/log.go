package cmd

import (
	"errors"
	"os"

	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/tty"
	"github.com/spf13/cobra"
)

var (
	logColorFlag  bool
	logNumberFlag int32
	logCmd        = &cobra.Command{
		Use:   "log",
		Short: "Show change history",
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

			// Use the pager to fetch and display the log output
			if err := ShowLogWithPager(c, logNumberFlag, &logColorFlag, false, func(s string) {
				cmd.Println(s)
			}); err != nil {
				return errors.Join(errors.New("display log"), err)
			}

			return nil
		},
	}
)

func init() {
	logCmd.Flags().BoolVar(&logColorFlag, "color", tty.IsInteractive(), "Enable colored output")
	logCmd.Flags().Int32VarP(&logNumberFlag, "number", "n", 10, "Maximum number of changes to display")
	rootCmd.AddCommand(logCmd)
}
