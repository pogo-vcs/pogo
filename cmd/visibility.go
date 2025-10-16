package cmd

import (
	"errors"
	"os"

	"github.com/pogo-vcs/pogo/client"
	"github.com/spf13/cobra"
)

var visibilityCmd = &cobra.Command{
	Use:   "visibility <public|private>",
	Short: "Set repository visibility to public or private",
	Long: `Set the repository's visibility to either public or private.

Public repositories can be accessed by anyone, while private repositories
require explicit access grants. Only users with access to the repository
can change its visibility.`,
	Example: `# Make repository public
pogo visibility public

# Make repository private
pogo visibility private`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var public bool
		switch args[0] {
		case "public":
			public = true
		case "private":
			public = false
		default:
			return errors.New("argument must be 'public' or 'private'")
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

		if err := c.SetRepositoryVisibility(public); err != nil {
			return errors.Join(errors.New("set repository visibility"), err)
		}

		return nil
	},
}

func init() {
	RootCmd.AddCommand(visibilityCmd)
}
