package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var Version = "dev"

var (
	globalNoPager   bool
	globalTimer     bool
	globalTimeStart time.Time

	rootCmd = &cobra.Command{
		Use:   "pogo",
		Short: "A brief description of your application",
		Long: `A longer description that spans multiple lines and likely contains
examples and usage of using your application. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if globalTimer {
				globalTimeStart = time.Now()
			}
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if globalTimer {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nTime: %s\n", time.Since(globalTimeStart))
			}
		},
	}
)

func init() {
	rootCmd.SilenceUsage = true
	rootCmd.DisableAutoGenTag = true

	// Add global flags
	rootCmd.PersistentFlags().BoolVar(&globalNoPager, "no-pager", false, "Disable pager for all output")
	rootCmd.PersistentFlags().BoolVar(&globalTimer, "time", false, "Measure command execution time")
}

func Execute() {
	rootCmd.Version = Version
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
