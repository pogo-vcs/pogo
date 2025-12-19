//go:build !nogui

package cmd

import (
	"errors"
	"os"

	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/gui"
	"github.com/spf13/cobra"
)

var guiCmd = &cobra.Command{
	Use:   "gui",
	Short: "Open the graphical user interface",
	Long: `Open a graphical window to browse and manage changes.

The GUI provides:
- A change graph showing the history of changes
- A file list showing changed files in the selected change
- A diff viewer with syntax highlighting

Controls:
- Click on a change to select it and see its files
- Click on a file to see its diff
- Scroll to navigate through changes and diffs`,
	Example: `# Open the GUI
pogo gui`,
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

		return gui.Run(cmd.Context(), c)
	},
}

func init() {
	RootCmd.AddCommand(guiCmd)
}
