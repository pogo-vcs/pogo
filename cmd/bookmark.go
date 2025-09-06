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
		Short:   "Manage bookmarks",
	}
	bookmarkSetCmd = &cobra.Command{
		Use:     "set",
		Aliases: []string{"s"},
		Short:   "Set a bookmark",
		Example: `pogo bookmark set main abc123`,
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

			if err := c.SetBookmark(bookmarkName, changeName); err != nil {
				return errors.Join(errors.New("set bookmark"), err)
			}

			return nil
		},
	}
	bookmarkListCmd = &cobra.Command{
		Use:     "list",
		Aliases: []string{"l"},
		Short:   "List all bookmarks",
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

			bookmarks, err := c.GetBookmarks()
			if err != nil {
				return errors.Join(errors.New("get bookmarks"), err)
			}

			if len(bookmarks) == 0 {
				fmt.Println("No bookmarks found")
				return nil
			}

			for _, bookmark := range bookmarks {
				fmt.Printf("%s -> %s\n", bookmark.Name, bookmark.ChangeName)
			}

			return nil
		},
	}
)

func init() {
	bookmarkCmd.AddCommand(bookmarkSetCmd)
	bookmarkCmd.AddCommand(bookmarkListCmd)

	rootCmd.AddCommand(bookmarkCmd)
}
