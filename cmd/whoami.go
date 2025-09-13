package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/pogo-vcs/pogo/auth"
	"github.com/pogo-vcs/pogo/client"
	"github.com/spf13/cobra"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show authentication information for the current repository",
	Long: `Show the personal access token being used for authentication with the current repository's server.

This command displays:
- The server URL this repository is connected to
- The personal access token used for authentication

Personal access tokens are stored securely in your system's keyring/keychain
and are associated with specific server URLs. Different repositories on the
same server share the same token.

This command is useful for:
- Debugging authentication issues
- Verifying which credentials are being used
- Checking server connectivity configuration
- Sharing tokens between team members (with caution)

Note: Personal access tokens should be kept secret. Only share them with
trusted team members who need access to the same repositories.`,
	Example: `# Show current authentication info
pogo whoami

# Example output:
# Server: pogo.example.com:8080
# Personal Access Token: yMq3CR3BvKR6VrXn7TdDmAtt9N6M3x7a`,
	RunE: func(cmd *cobra.Command, args []string) error {
		wd, err := os.Getwd()
		if err != nil {
			return errors.Join(errors.New("get working directory"), err)
		}

		// Find repo file to get server
		file, err := client.FindRepoFile(wd)
		if err != nil {
			return errors.Join(errors.New("find repo file - not in a pogo repository"), err)
		}

		repo := &client.Repo{}
		if err := repo.Load(file); err != nil {
			return errors.Join(errors.New("load repo file"), err)
		}

		// Get token for this server
		token, err := client.GetToken(repo.Server)
		if err != nil {
			return errors.Join(errors.New("get token"), err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Server: %s\n", repo.Server)
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Personal Access Token: %s\n", auth.Encode(token))
		return nil
	},
}

func init() {
	RootCmd.AddCommand(whoamiCmd)
}
