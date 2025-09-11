package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/pogo-vcs/pogo/db"
	"github.com/pogo-vcs/pogo/server"
	"github.com/pogo-vcs/pogo/server/env"
	"github.com/robfig/cron/v3"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start a Pogo server",
	Long: `Start a Pogo server to host repositories and handle client connections.

The server provides:
- gRPC API for Pogo clients (version control operations)
- HTTP web interface for browsing repositories
- Go module proxy support for importing Pogo repos as Go modules
- Automatic daily garbage collection at 3 AM
- PostgreSQL backend for metadata storage
- File-based object storage for content

Configuration:
  The server can be configured through environment variables:
- HOST - Full host:port binding (e.g., "0.0.0.0:8080")
- PORT - Port number only (binds to all interfaces)
- DATABASE_URL - PostgreSQL connection string
- OBJECT_STORAGE_PATH - Directory for storing file objects
- GC_MEMORY_THRESHOLD - File count threshold for GC strategy

The server requires a PostgreSQL database to be running and accessible.
On first run, it will automatically set up the required database schema.

Security:
- Authentication via personal access tokens
- Public repositories allow read-only access without auth
- All write operations require authentication`,
	Example: `# Start server on default port 8080
pogo serve

# Start server on custom port
PORT=3000 pogo serve

# Start server with specific host binding
HOST=192.168.1.100:8080 pogo serve

# Start with PostgreSQL configuration
DATABASE_URL=postgres://user:pass@localhost/pogo pogo serve

# Docker example
docker run -p 8080:8080 -e DATABASE_URL=... ghcr.io/pogo-vcs/pogo:alpine`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := env.InitFromEnvironment(); err != nil {
			return err
		}

		db.Connect()

		srv := server.NewServer()
		defer srv.Stop(cmd.Context())

		if err := srv.Start(env.ListenAddress); err != nil {
			return err
		}

		// Set up cron job for automatic garbage collection at 3 AM daily
		c := cron.New()
		_, err := c.AddFunc("0 3 * * *", func() {
			fmt.Fprintln(os.Stderr, "Running scheduled garbage collection...")
			ctx := context.Background()
			resp, err := server.RunGarbageCollection(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error during scheduled garbage collection: %v\n", err)
				return
			}
			fmt.Fprintf(os.Stderr, "Scheduled GC completed: deleted %d database files, %d disk files, freed %d bytes\n",
				resp.DeletedDatabaseFiles, resp.DeletedDiskFiles, resp.BytesFreed)
		})
		if err != nil {
			_, _ = fmt.Fprintf(cmd.OutOrStderr(), "Warning: Failed to schedule automatic garbage collection: %v\n", err)
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
