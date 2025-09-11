package cmd

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/pogo-vcs/pogo/auth"
	"github.com/pogo-vcs/pogo/client"
	"github.com/spf13/cobra"
)

var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Manage personal access tokens",
	Long: `Manage personal access tokens stored in the system keyring for different Pogo servers.

Personal access tokens are used to authenticate with Pogo servers. They are
stored securely in your system's keyring/keychain, which means:
- Tokens persist across terminal sessions
- Tokens are encrypted at rest
- Each server has its own token
- Tokens are shared across all repositories on the same server

You typically receive a token from:
- Your system administrator when joining a team
- The server's web interface after logging in
- Another team member (share securely!)

The token is automatically used for all operations with the associated server.`,
}

var tokenSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set or update a personal access token for a server",
	Long: `Set or update a personal access token for a specific Pogo server.

The token will be securely stored in your system's keyring/keychain and
automatically used for all future operations with that server.

If run from within a repository, the server address will be automatically
detected. Otherwise, use the --server flag to specify the server.

The command will:
- Prompt you to enter the token interactively (for security)
- Validate the token format
- Store it securely in your system keyring
- Confirm successful storage`,
	Example: `  # Set token for current repository's server (run from within repo)
  pogo token set

  # Set token for a specific server
  pogo token set --server pogo.example.com:8080

  # The command will prompt:
  # Enter the personal access token for pogo.example.com:8080
  # > yMq3CR3BvKR6VrXn7TdDmAtt9N6M3x7a`,
	RunE: func(cmd *cobra.Command, args []string) error {
		server := cmd.Flag("server").Value.String()

		// If no server specified, try to get it from current repository
		if server == "" {
			wd, err := os.Getwd()
			if err == nil {
				if file, err := client.FindRepoFile(wd); err == nil {
					repo := &client.Repo{}
					if err := repo.Load(file); err == nil {
						server = repo.Server
					}
				}
			}

			if server == "" {
				return fmt.Errorf("server is required (use --server flag or run from within a repository)")
			}
		}

		// Check if token already exists
		existingToken, _ := client.GetToken(server)

		title := "Set Personal Access Token"
		description := fmt.Sprintf("Enter the personal access token for %s", server)
		if existingToken != nil {
			title = "Update Personal Access Token"
			description = fmt.Sprintf("Enter a new personal access token for %s (this will overwrite the existing one)", server)
		}

		var tokenStr string
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title(title).
					Description(description).
					Placeholder("Enter your token here").
					Value(&tokenStr),
			),
		)

		if err := form.Run(); err != nil {
			return fmt.Errorf("run form: %w", err)
		}

		if tokenStr == "" {
			return fmt.Errorf("no token provided")
		}

		token, err := auth.Decode(tokenStr)
		if err != nil {
			return fmt.Errorf("failed to decode token: %w", err)
		}

		// Store in keyring
		if err := client.SetToken(server, token); err != nil {
			return fmt.Errorf("failed to store token: %w", err)
		}

		if existingToken != nil {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Token updated successfully for %s\n", server)
		} else {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Token set successfully for %s\n", server)
		}

		return nil
	},
}

var tokenRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove a personal access token for a server",
	Long: `Remove a personal access token for a specific Pogo server from the system keyring.

This will:
- Delete the stored token for the specified server
- Require re-authentication for future operations
- Ask for confirmation before deletion

Use this command when:
- Leaving a team or organization
- Rotating credentials for security
- Troubleshooting authentication issues
- Cleaning up old server configurations`,
	Example: `# Remove token for current repository's server (run from within repo)
pogo token remove

# Remove token for a specific server
pogo token remove --server old.server.com:8080

# The command will ask for confirmation:
# Are you sure you want to remove the token for old.server.com:8080?
# > Yes`,
	RunE: func(cmd *cobra.Command, args []string) error {
		server := cmd.Flag("server").Value.String()

		// If no server specified, try to get it from current repository
		if server == "" {
			wd, err := os.Getwd()
			if err == nil {
				if file, err := client.FindRepoFile(wd); err == nil {
					repo := &client.Repo{}
					if err := repo.Load(file); err == nil {
						server = repo.Server
					}
				}
			}

			if server == "" {
				return fmt.Errorf("server is required (use --server flag or run from within a repository)")
			}
		}

		// Check if token exists
		_, err := client.GetToken(server)
		if err != nil {
			return fmt.Errorf("no token found for server %s", server)
		}

		// Confirm deletion
		var confirmDelete bool
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Remove Personal Access Token").
					Description(fmt.Sprintf("Are you sure you want to remove the token for %s?", server)).
					Value(&confirmDelete),
			),
		)

		if err := form.Run(); err != nil {
			return fmt.Errorf("run form: %w", err)
		}

		if !confirmDelete {
			_, _ = fmt.Fprintf(cmd.OutOrStderr(), "Token removal cancelled")
			return nil
		}

		// Remove from keyring using empty token
		if err := client.RemoveToken(server); err != nil {
			return fmt.Errorf("failed to remove token: %w", err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Token removed successfully for %s\n", server)
		return nil
	},
}

func init() {
	// Add flags to set command
	tokenSetCmd.Flags().String("server", "", "Pogo server address (host:port), defaults to server from current repository")

	// Add flags to remove command
	tokenRemoveCmd.Flags().String("server", "", "Pogo server address (host:port), defaults to server from current repository")

	// Add subcommands to token command
	tokenCmd.AddCommand(tokenSetCmd)
	tokenCmd.AddCommand(tokenRemoveCmd)

	// Add token command to root
	rootCmd.AddCommand(tokenCmd)
}
