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
	Long: `Run garbage collection on the server to clean up unreachable data.
This command will:
- Remove unreachable files from the database
- Delete orphaned objects from the filesystem
- Report the amount of space freed`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		// Open client connection
		c, err := client.OpenFromFile(ctx, ".")
		if err != nil {
			return fmt.Errorf("open client: %w", err)
		}
		defer c.Close()

		// Run garbage collection
		fmt.Println("Running garbage collection...")
		resp, err := c.GarbageCollect(ctx)
		if err != nil {
			return fmt.Errorf("garbage collect: %w", err)
		}

		// Report results
		fmt.Printf("Garbage collection completed:\n")
		fmt.Printf("  Database files deleted: %d\n", resp.DeletedDatabaseFiles)
		fmt.Printf("  Disk files deleted: %d\n", resp.DeletedDiskFiles)
		fmt.Printf("  Bytes freed: %d\n", resp.BytesFreed)

		if resp.BytesFreed > 0 {
			// Convert bytes to human-readable format
			size := float64(resp.BytesFreed)
			units := []string{"B", "KB", "MB", "GB", "TB"}
			unitIdx := 0
			for size >= 1024 && unitIdx < len(units)-1 {
				size /= 1024
				unitIdx++
			}
			fmt.Printf("  Space freed: %.2f %s\n", size, units[unitIdx])
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(gcCmd)
}
