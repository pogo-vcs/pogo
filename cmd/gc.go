package cmd

import (
	"context"
	"fmt"

	"github.com/pogo-vcs/pogo/client"
	"github.com/spf13/cobra"
)

var gcCmd = &cobra.Command{
	Use:   "gc",
	Short: "Run garbage collection on the server",
	Long: `Run garbage collection on the Pogo server to clean up unreachable data.

Garbage collection removes data that is no longer referenced by any repository,
freeing up disk space and keeping the server storage efficient. This includes:
  • Unreachable file records in the database
  • Orphaned file objects on the filesystem
  • Temporary data from interrupted operations

The GC process is safe and will never remove:
  • Files referenced by any change in any repository
  • Recent uploads (within the safety window)
  • Active bookmarks or their history

The server automatically runs GC daily at 3 AM, but you can trigger it
manually when needed. The operation uses an adaptive strategy that
automatically chooses the most efficient approach based on data size.

Requirements:
  • You must be authenticated to run GC
  • The command must be run from within a Pogo repository`,
	Example: `pogo gc

# Example output:
# Running garbage collection...
# Garbage collection completed:
#   Database files deleted: 1523
#   Disk files deleted: 1523
#   Bytes freed: 15728640
#   Space freed: 15.00 MB`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		// Open client connection
		c, err := client.OpenFromFile(ctx, ".")
		if err != nil {
			return fmt.Errorf("open client: %w", err)
		}
		defer c.Close()
		configureClientOutputs(cmd, c)

		// Run garbage collection
		fmt.Fprintln(c.VerboseOut, "Running garbage collection...")
		resp, err := c.GarbageCollect(ctx)
		if err != nil {
			return fmt.Errorf("run garbage collection: %w", err)
		}

		// Print results
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Garbage collection completed:\n")
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  Database files deleted: %d\n", resp.DeletedDatabaseFiles)
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  Disk files deleted: %d\n", resp.DeletedDiskFiles)
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  Bytes freed: %d\n", resp.BytesFreed)

		if resp.BytesFreed > 0 {
			// Convert bytes to human-readable format
			size := float64(resp.BytesFreed)
			units := []string{"B", "KB", "MB", "GB", "TB"}
			unitIdx := 0
			for size >= 1024 && unitIdx < len(units)-1 {
				size /= 1024
				unitIdx++
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  Space freed: %.2f %s\n", size, units[unitIdx])
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(gcCmd)
}
