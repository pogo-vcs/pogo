package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/pogo-vcs/pogo/db"
	"github.com/pogo-vcs/pogo/server"
	"github.com/robfig/cron/v3"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var host string
		if hostEnv, ok := os.LookupEnv("HOST"); ok {
			host = hostEnv
		} else if portEnv, ok := os.LookupEnv("PORT"); ok {
			host = ":" + portEnv
		} else if cmd.Flags().Changed("host") {
			host = cmd.Flag("host").Value.String()
		} else if cmd.Flags().Changed("port") {
			host = ":" + cmd.Flag("port").Value.String()
		} else {
			host = ":8080"
		}

		db.Connect()

		srv := server.NewServer()
		defer srv.Stop(cmd.Context())

		if err := srv.Start(host); err != nil {
			return err
		}

		// Set up cron job for automatic garbage collection at 3 AM daily
		c := cron.New()
		_, err := c.AddFunc("0 3 * * *", func() {
			fmt.Println("Running scheduled garbage collection...")
			ctx := context.Background()
			resp, err := server.RunGarbageCollection(ctx)
			if err != nil {
				fmt.Printf("Error during scheduled garbage collection: %v\n", err)
				return
			}
			fmt.Printf("Scheduled GC completed: deleted %d database files, %d disk files, freed %d bytes\n",
				resp.DeletedDatabaseFiles, resp.DeletedDiskFiles, resp.BytesFreed)
		})
		if err != nil {
			cmd.Printf("Warning: Failed to schedule automatic garbage collection: %v\n", err)
		} else {
			c.Start()
			defer c.Stop()
			cmd.Println("Automatic garbage collection scheduled for 3 AM daily")
		}

		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGABRT)
		<-sig
		cmd.Println("Shutting down...")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
