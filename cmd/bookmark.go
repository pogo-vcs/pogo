package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/pogo-vcs/pogo/client"
	"github.com/spf13/cobra"
)

var (
	bookmarkCmd = &cobra.Command{
		Use:     "bookmark",
		Aliases: []string{"b"},
		Short:   "Manage bookmarks for changes",
		Long: `Manage bookmarks that point to specific changes in your repository.

Bookmarks in Pogo are similar to tags and branches in Git combined. They are
named references to specific changes that make it easy to find important
versions of your code.

Common bookmark patterns:
- "main" - The current production/stable version (treated specially by Pogo)
- "v1.0.0", "v2.1.3" - Semantic version tags for releases
- "feature-xyz" - Mark the completion of a feature
- "before-refactor" - Mark a point before major changes

The "main" bookmark is special, it's treated as the default branch and is
what new users will see when they clone your repository.

This command pushes any changes before running.`,
	}
	bookmarkSetCmd = &cobra.Command{
		Use:     "set <name> [change]",
		Aliases: []string{"s"},
		Short:   "Set a bookmark to point to a specific change",
		Long: `Set a bookmark to point to a specific change.

If no change is specified, the bookmark will point to the current change.
Setting a bookmark to a change that already has the bookmark will move it.

Bookmarks make changes read-only by default to preserve history. Use the
push --force flag if you need to modify a bookmarked change.`,
		Example: `# Set "main" bookmark to current change
pogo bookmark set main

# Set "v1.0.0" bookmark to current change
pogo bookmark set v1.0.0

# Set "main" bookmark to a specific change
pogo bookmark set main sunny-sunset-42

# Move an existing bookmark to current change
pogo bookmark set production

# Using the short alias
pogo b s main`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				bookmarkName string
				changeName   *string
			)

			switch len(args) {
			case 0:
				return errors.New("bookmark name is required")
			case 1:
				bookmarkName = args[0]
				// change name is current checked out change
				changeName = nil
			case 2:
				bookmarkName = args[0]
				changeName = &args[1]
			default:
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

			if err := c.SetBookmark(bookmarkName, changeName); err != nil {
				return errors.Join(errors.New("set bookmark"), err)
			}

			return nil
		},
	}
	bookmarkListCmd = &cobra.Command{
		Use:     "list",
		Aliases: []string{"l"},
		Short:   "List all bookmarks in the repository",
		Long: `List all bookmarks in the repository along with the changes they point to.

This shows you all named references in your repository, making it easy to
see important versions, releases, and the current "main" branch.`,
		Example: `  # List all bookmarks
  pogo bookmark list

  # Using the short alias
  pogo b l`,
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

			bookmarks, err := c.GetBookmarks()
			if err != nil {
				return errors.Join(errors.New("get bookmarks"), err)
			}

			if len(bookmarks) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStderr(), "No bookmarks found")
				return nil
			}

			for _, bookmark := range bookmarks {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s -> %s\n", bookmark.Name, bookmark.ChangeName)
			}

			return nil
		},
	}
	bookmarkRemoveCmd = &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm", "delete", "del"},
		Short:   "Remove a bookmark from the repository",
		Long: `Remove a bookmark from the repository.

This permanently removes the named reference to a change, but does not
delete the change itself. The change will still exist and can be
accessed by its change name.`,
		Example: `  # Remove a bookmark
  pogo bookmark remove old-version

  # Remove the main bookmark (be careful!)
  pogo bookmark remove main

  # Using the short alias
  pogo b rm v1.0.0`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("bookmark name is required")
			}
			if len(args) > 1 {
				return errors.New("too many arguments")
			}

			bookmarkName := args[0]

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

			if err := c.RemoveBookmark(bookmarkName); err != nil {
				return errors.Join(errors.New("remove bookmark"), err)
			}

			return nil
		},
	}
)

func init() {
	bookmarkCmd.AddCommand(bookmarkSetCmd)
	bookmarkCmd.AddCommand(bookmarkListCmd)
	bookmarkCmd.AddCommand(bookmarkRemoveCmd)

	rootCmd.AddCommand(bookmarkCmd)
}
