package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/pogo-vcs/pogo/client"
	"github.com/spf13/cobra"
)

var (
	secretsCmd = &cobra.Command{
		Use:   "secrets",
		Short: "Manage repository secrets for CI pipelines",
		Long: `Manage secrets that can be accessed in CI pipeline configurations.

Secrets are encrypted values that can be referenced in your CI pipeline YAML
files using the {{ secret "KEY" }} template function. They are useful for
storing sensitive data like API tokens, deployment keys, and credentials.

Secrets are scoped to a repository and can only be accessed by users with
access to that repository.`,
	}
	secretsListCmd = &cobra.Command{
		Use:     "list",
		Aliases: []string{"l"},
		Short:   "List all secrets in the repository",
		Long: `List all secrets in the repository.

This shows the keys of all secrets, but not their values for security reasons.`,
		Example: `  # List all secrets
  pogo secrets list

  # Using the short alias
  pogo secrets l`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return errors.New("too many arguments")
			}

			wd, err := os.Getwd()
			if err != nil {
				return errors.Join(errors.New("get working directory"), err)
			}
			c, err := client.OpenFromFile(cmd.Context(), wd)
			if err != nil {
				return errors.Join(errors.New("open client"), err)
			}
			defer c.Close()
			configureClientOutputs(cmd, c)

			secrets, err := c.GetAllSecrets()
			if err != nil {
				return errors.Join(errors.New("get secrets"), err)
			}

			if len(secrets) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStderr(), "No secrets found")
				return nil
			}

			for _, secret := range secrets {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", secret.Key)
			}

			return nil
		},
	}
	secretsGetCmd = &cobra.Command{
		Use:     "get <key>",
		Aliases: []string{"g"},
		Short:   "Get the value of a secret",
		Long: `Get the value of a secret by its key.

This will display the secret value in plain text, so be careful when using
this command in shared or recorded terminal sessions.`,
		Example: `  # Get a secret value
  pogo secrets get DEPLOY_TOKEN

  # Using the short alias
  pogo secrets g API_KEY`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("secret key is required")
			}
			if len(args) > 1 {
				return errors.New("too many arguments")
			}

			key := args[0]

			wd, err := os.Getwd()
			if err != nil {
				return errors.Join(errors.New("get working directory"), err)
			}
			c, err := client.OpenFromFile(cmd.Context(), wd)
			if err != nil {
				return errors.Join(errors.New("open client"), err)
			}
			defer c.Close()
			configureClientOutputs(cmd, c)

			value, err := c.GetSecret(key)
			if err != nil {
				return errors.Join(errors.New("get secret"), err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", value)

			return nil
		},
	}
	secretsSetCmd = &cobra.Command{
		Use:     "set <key> <value>",
		Aliases: []string{"s"},
		Short:   "Set a secret value",
		Long: `Set a secret value for the repository.

If a secret with the same key already exists, it will be updated with the
new value. Secrets can be used in CI pipeline configurations.`,
		Example: `  # Set a secret
  pogo secrets set DEPLOY_TOKEN abc123xyz

  # Update an existing secret
  pogo secrets set API_KEY new-key-value

  # Using the short alias
  pogo secrets s DATABASE_URL postgres://...`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				return errors.New("secret key and value are required")
			}
			if len(args) > 2 {
				return errors.New("too many arguments")
			}

			key := args[0]
			value := args[1]

			wd, err := os.Getwd()
			if err != nil {
				return errors.Join(errors.New("get working directory"), err)
			}
			c, err := client.OpenFromFile(cmd.Context(), wd)
			if err != nil {
				return errors.Join(errors.New("open client"), err)
			}
			defer c.Close()
			configureClientOutputs(cmd, c)

			if err := c.SetSecret(key, value); err != nil {
				return errors.Join(errors.New("set secret"), err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Secret '%s' set successfully\n", key)

			return nil
		},
	}
	secretsDeleteCmd = &cobra.Command{
		Use:     "delete <key>",
		Aliases: []string{"d", "rm", "remove"},
		Short:   "Delete a secret",
		Long: `Delete a secret from the repository.

This permanently removes the secret. Any CI pipelines that reference this
secret will receive an empty string when accessing it.`,
		Example: `  # Delete a secret
  pogo secrets delete OLD_TOKEN

  # Using the short alias
  pogo secrets d UNUSED_KEY`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("secret key is required")
			}
			if len(args) > 1 {
				return errors.New("too many arguments")
			}

			key := args[0]

			wd, err := os.Getwd()
			if err != nil {
				return errors.Join(errors.New("get working directory"), err)
			}
			c, err := client.OpenFromFile(cmd.Context(), wd)
			if err != nil {
				return errors.Join(errors.New("open client"), err)
			}
			defer c.Close()
			configureClientOutputs(cmd, c)

			if err := c.DeleteSecret(key); err != nil {
				return errors.Join(errors.New("delete secret"), err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Secret '%s' deleted successfully\n", key)

			return nil
		},
	}
)

func init() {
	secretsCmd.AddCommand(secretsListCmd)
	secretsCmd.AddCommand(secretsGetCmd)
	secretsCmd.AddCommand(secretsSetCmd)
	secretsCmd.AddCommand(secretsDeleteCmd)

	RootCmd.AddCommand(secretsCmd)
}
