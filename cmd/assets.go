package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/pogo-vcs/pogo/client"
	"github.com/spf13/cobra"
)

var (
	assetsCmd = &cobra.Command{
		Use:   "assets",
		Short: "Manage repository assets (release binaries, etc.)",
		Long: `Manage assets associated with the repository.

Assets are files that can be uploaded and downloaded from the server,
typically used for release binaries, documentation, or other artifacts
produced by CI pipelines.

Assets are stored per-repository and can be accessed publicly via HTTP.
Writing and deleting assets requires authentication.`,
	}
	assetsListCmd = &cobra.Command{
		Use:     "list",
		Aliases: []string{"l", "ls"},
		Short:   "List all assets in the repository",
		Long: `List all assets in the repository.

This shows all uploaded assets with their names/paths.`,
		Example: `  # List all assets
  pogo assets list

  # Using the short alias
  pogo assets ls`,
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

			assets, err := c.ListAssets()
			if err != nil {
				return errors.Join(errors.New("list assets"), err)
			}

			if len(assets) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStderr(), "No assets found")
				return nil
			}

			for _, asset := range assets {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", asset)
			}

			return nil
		},
	}
	assetsGetCmd = &cobra.Command{
		Use:     "get <name>",
		Aliases: []string{"g", "cat"},
		Short:   "Download an asset to stdout",
		Long: `Download an asset and write its contents to stdout.

This is useful for piping asset contents to other commands or for
inspecting small text-based assets.`,
		Example: `  # Download an asset to stdout
  pogo assets get release/v1.0/binary

  # Save to a file
  pogo assets get release/v1.0/binary > binary

  # Using the short alias
  pogo assets cat README.md`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("asset name is required")
			}
			if len(args) > 1 {
				return errors.New("too many arguments")
			}

			name := args[0]

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

			if err := c.GetAsset(name, cmd.OutOrStdout()); err != nil {
				return errors.Join(errors.New("get asset"), err)
			}

			return nil
		},
	}
	assetsPutCmd = &cobra.Command{
		Use:     "put <name> [file]",
		Aliases: []string{"p", "upload"},
		Short:   "Upload an asset",
		Long: `Upload an asset to the repository.

If a file path is provided, the file's contents are uploaded.
If no file is provided, the asset content is read from stdin.

Asset names can contain slashes to create a directory-like structure
(e.g., "release/v1.0/binary").`,
		Example: `  # Upload from a file
  pogo assets put release/v1.0/binary ./build/output

  # Upload from stdin
  cat ./build/output | pogo assets put release/v1.0/binary

  # Using the short alias
  pogo assets upload docs/README.md ./README.md`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("asset name is required")
			}
			if len(args) > 2 {
				return errors.New("too many arguments")
			}

			name := args[0]
			var reader io.Reader

			if len(args) == 2 {
				// Read from file
				file, err := os.Open(args[1])
				if err != nil {
					return errors.Join(errors.New("open file"), err)
				}
				defer file.Close()
				reader = file
			} else {
				// Read from stdin
				reader = cmd.InOrStdin()
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

			if err := c.PutAsset(name, reader); err != nil {
				return errors.Join(errors.New("put asset"), err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Asset '%s' uploaded successfully\n", name)

			return nil
		},
	}
	assetsDeleteCmd = &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"d", "rm", "remove"},
		Short:   "Delete an asset",
		Long: `Delete an asset from the repository.

This permanently removes the asset from the server.`,
		Example: `  # Delete an asset
  pogo assets delete release/v0.9/binary

  # Using the short alias
  pogo assets rm old-release.tar.gz`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("asset name is required")
			}
			if len(args) > 1 {
				return errors.New("too many arguments")
			}

			name := args[0]

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

			if err := c.DeleteAsset(name); err != nil {
				return errors.Join(errors.New("delete asset"), err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Asset '%s' deleted successfully\n", name)

			return nil
		},
	}
)

func init() {
	assetsCmd.AddCommand(assetsListCmd)
	assetsCmd.AddCommand(assetsGetCmd)
	assetsCmd.AddCommand(assetsPutCmd)
	assetsCmd.AddCommand(assetsDeleteCmd)

	RootCmd.AddCommand(assetsCmd)
}
