package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pogo-vcs/pogo/client"
	"github.com/spf13/cobra"
)

var cloneCmd = &cobra.Command{
	Use:   "clone",
	Short: "Clone a repository from a Pogo server",
	Long: `Clone a repository from a Pogo server to a local directory.

This command creates a new directory (or uses the specified directory) and downloads
all files from the specified repository. By default, it will download the "main"
bookmark if it exists, otherwise it will use the root change (the first change
without any parent).

You can specify a specific revision (change name or bookmark) to clone instead of
the default behavior. The revision parameter uses the same fuzzy matching as the
edit command:
- Exact bookmark matches take priority
- Then exact change name matches
- Finally, change name prefix matches

The cloned repository will be configured to track the original repository, allowing
you to push changes back using the standard Pogo commands.`,
	Example: `# Clone the main bookmark from a repository
pogo clone --server localhost:8080 --repo my-project

# Clone to a specific directory
pogo clone --server pogo.example.com:8080 --repo open-source-project --dir ./my-local-copy

# Clone a specific revision
pogo clone --server localhost:8080 --repo my-project --dir ./project --revision v1.0.0

# Clone a specific change by name
pogo clone --server localhost:8080 --repo my-project --revision KPHRpdJnwyPcLH4a`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("unexpected arguments: %v (use flags instead)", args)
		}

		server := cmd.Flag("server").Value.String()
		repoName := cmd.Flag("repo").Value.String()
		targetDir := cmd.Flag("dir").Value.String()
		revision := cmd.Flag("revision").Value.String()

		if server == "" {
			return fmt.Errorf("--server flag is required")
		}
		if repoName == "" {
			return fmt.Errorf("--repo flag is required")
		}

		// Default to repository name if no directory specified
		if targetDir == "" {
			targetDir = repoName
		}

		// Convert to absolute path
		absTargetDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("get absolute path for '%s': %w", targetDir, err)
		}

		// Check if target directory exists and is not empty
		if info, err := os.Stat(absTargetDir); err == nil {
			if info.IsDir() {
				entries, err := os.ReadDir(absTargetDir)
				if err != nil {
					return fmt.Errorf("read target directory '%s': %w", absTargetDir, err)
				}
				if len(entries) > 0 {
					return fmt.Errorf("target directory '%s' is not empty", absTargetDir)
				}
			} else {
				return fmt.Errorf("target path '%s' exists but is not a directory", absTargetDir)
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("check target directory '%s': %w", absTargetDir, err)
		}

		// Create target directory if it doesn't exist
		if err := os.MkdirAll(absTargetDir, 0755); err != nil {
			return fmt.Errorf("create target directory '%s': %w", absTargetDir, err)
		}

		// Create client to get repository information
		c, err := client.OpenNew(cmd.Context(), server, absTargetDir)
		if err != nil {
			return fmt.Errorf("open client: %w", err)
		}
		defer c.Close()
		configureClientOutputs(cmd, c)

		// Get repository information
		repoInfo, err := c.GetRepositoryInfo(repoName)
		if err != nil {
			return fmt.Errorf("get repository info for '%s': %w", repoName, err)
		}

		// Determine which revision to use if not specified
		if revision == "" {
			if repoInfo.MainBookmarkChange != nil {
				revision = *repoInfo.MainBookmarkChange
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Cloning main bookmark (%s)\n", revision)
			} else if repoInfo.RootChange != nil {
				revision = *repoInfo.RootChange
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Cloning root change (%s)\n", revision)
			} else {
				return fmt.Errorf("repository '%s' appears to be empty (no main bookmark or root change)", repoName)
			}
		}

		// Create .pogo.db config file
		repoStore, err := client.CreateRepoStore(absTargetDir, server, repoInfo.RepoId, 0)
		if err != nil {
			return fmt.Errorf("create repo store: %w", err)
		}
		repoStore.Close()

		// Open client from the config file we just created
		c2, err := client.OpenFromFile(cmd.Context(), absTargetDir)
		if err != nil {
			return fmt.Errorf("open client from config: %w", err)
		}
		defer c2.Close()
		configureClientOutputs(cmd, c2)

		// Use Edit function to download files
		if err := c2.Edit(revision); err != nil {
			return fmt.Errorf("download files for revision '%s': %w", revision, err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Repository '%s' cloned to '%s'\n", repoName, absTargetDir)
		return nil
	},
}

func init() {
	cloneCmd.Flags().String("server", "", "server address (host:port)")
	_ = cloneCmd.MarkFlagRequired("server")

	cloneCmd.Flags().String("repo", "", "repository name")
	_ = cloneCmd.MarkFlagRequired("repo")

	cloneCmd.Flags().String("dir", "", "target directory (defaults to repository name)")

	cloneCmd.Flags().String("revision", "", "specific revision to clone (bookmark or change name)")

	RootCmd.AddCommand(cloneCmd)
}
