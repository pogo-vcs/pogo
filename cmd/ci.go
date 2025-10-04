package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/server/ci"
	"github.com/spf13/cobra"
)

var (
	ciCmd = &cobra.Command{
		Use:   "ci",
		Short: "Manage CI pipelines",
		Long:  `Commands for working with CI pipelines`,
	}

	ciTestCmd = &cobra.Command{
		Use:   "test [config-file]",
		Short: "Test a CI pipeline configuration",
		Long: `Test a CI pipeline configuration by executing it with a synthetic event.

This command allows you to test your CI pipeline locally before pushing it to the server.
It will execute the pipeline using the same logic as the server would, allowing you to verify
that your configuration works as expected.

The config file should be a YAML file in the .pogo/ci/ directory.
If no config file is specified, all CI config files in .pogo/ci/ will be tested.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}

			repoFile, err := client.FindRepoFile(cwd)
			if err != nil {
				return fmt.Errorf("find repository: %w", err)
			}
			repoRoot := filepath.Dir(repoFile)

			configFiles := make(map[string][]byte)

			if len(args) == 1 {
				configPath := args[0]
				content, err := os.ReadFile(configPath)
				if err != nil {
					return fmt.Errorf("read config file: %w", err)
				}
				configFiles[filepath.Base(configPath)] = content
			} else {
				ciDir := filepath.Join(repoRoot, ".pogo", "ci")
				entries, err := os.ReadDir(ciDir)
				if err != nil {
					return fmt.Errorf("read CI directory: %w", err)
				}

				for _, entry := range entries {
					if entry.IsDir() {
						continue
					}

					ext := filepath.Ext(entry.Name())
					if ext != ".yaml" && ext != ".yml" {
						continue
					}

					content, err := os.ReadFile(filepath.Join(ciDir, entry.Name()))
					if err != nil {
						return fmt.Errorf("read config file %s: %w", entry.Name(), err)
					}
					configFiles[entry.Name()] = content
				}
			}

			if len(configFiles) == 0 {
				return fmt.Errorf("no CI configuration files found")
			}

			event := ci.Event{
				Rev:        ciTestEventRev,
				ArchiveUrl: ciTestEventArchiveURL,
			}

			executor := ci.NewExecutor()
			executor.SetRepoContentDir(repoRoot)

			eventType := ci.EventTypePush
			if ciTestEventType == "remove" {
				eventType = ci.EventTypeRemove
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Testing CI pipeline with synthetic %s event (rev: %s)\n", ciTestEventType, ciTestEventRev)
			fmt.Fprintf(cmd.OutOrStdout(), "Found %d configuration file(s)\n\n", len(configFiles))

			err = executor.ExecuteForBookmarkEvent(context.Background(), configFiles, event, eventType)
			if err != nil {
				return fmt.Errorf("execute CI: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "\n✓ CI pipeline test completed successfully\n")
			return nil
		},
	}

	ciTestEventType       string
	ciTestEventRev        string
	ciTestEventArchiveURL string
)

func init() {
	RootCmd.AddCommand(ciCmd)
	ciCmd.AddCommand(ciTestCmd)

	ciTestCmd.Flags().StringVarP(&ciTestEventType, "event-type", "t", "push", "Event type to simulate (push or remove)")
	ciTestCmd.Flags().StringVarP(&ciTestEventRev, "rev", "r", "main", "Revision name for the event")
	ciTestCmd.Flags().StringVarP(&ciTestEventArchiveURL, "archive-url", "a", "https://example.com/archive", "Archive URL for the event")
}
