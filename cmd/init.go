package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pogo-vcs/pogo/client"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new repository on a given Pogo server",
	Long: `Initialize a new Pogo repository in the current directory.

This command creates a new repository on the specified Pogo server and configures
the current directory to track it. A .pogo.yaml file will be created to store
the repository configuration.

The repository can be made public (read-only access for everyone) or kept private
(requires authentication for all access).`,
	Example: `# Initialize a private repository
pogo init --server localhost:8080 --name my-project

# Initialize a public repository
pogo init --server pogo.example.com:8080 --name open-source-project --public`,
	RunE: func(cmd *cobra.Command, args []string) error {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
		c, err := client.OpenNew(cmd.Context(), cmd.Flag("server").Value.String(), wd)
		if err != nil {
			return fmt.Errorf("open client: %w", err)
		}
		defer c.Close()
		configureClientOutputs(cmd, c)

		repoId, changeId, err := c.Init(
			cmd.Flag("name").Value.String(),
			cmd.Flag("public").Changed,
		)
		if err != nil {
			return fmt.Errorf("init repository: %w", err)
		}

		success := false
		defer func() {
			if !success {
				fmt.Printf("Deleting repository...\n")
				if err := c.DeleteRepo(); err != nil {
					fmt.Printf("warning: failed to delete repository: %v\n", err)
				} else {
					fmt.Printf("Repository deleted\n")
				}
			}
		}()

		if err := c.PushFull(false); err != nil {
			return fmt.Errorf("push full: %w", err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Repository ID: %d\n", repoId)

		if err := (&client.Repo{
			Server:   cmd.Flag("server").Value.String(),
			RepoId:   repoId,
			ChangeId: changeId,
		}).Save(filepath.Join(wd, ".pogo.yaml")); err != nil {
			return fmt.Errorf("save repo file: %w", err)
		}

		success = true

		return nil
	},
}

func init() {
	initCmd.Flags().String("server", "", "host:port")
	_ = initCmd.MarkFlagRequired("server")

	initCmd.Flags().String("name", "", "repository name")
	_ = initCmd.MarkFlagRequired("name")

	initCmd.Flags().Bool("public", false, "make repository public")

	RootCmd.AddCommand(initCmd)
}
