package cmd

import (
	"encoding/base64"
	"errors"
	"os"

	"github.com/pogo-vcs/pogo/client"
	"github.com/spf13/cobra"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show personal access token for current repository",
	Long:  `Show the personal access token being used for the current repository`,
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

		cmd.Printf("Server: %s\n", repo.Server)
		cmd.Printf("Personal Access Token: %s\n", base64.StdEncoding.EncodeToString(token))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(whoamiCmd)
}
