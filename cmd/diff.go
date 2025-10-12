package cmd

import (
	"errors"
	"os"

	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/tty"
	"github.com/spf13/cobra"
)

var (
	diffColorFlag bool
	diffCmd       = &cobra.Command{
		Use:   "diff [rev1] [rev2]",
		Short: "Show differences between changes",
		Long: `Display differences between changes in unified diff format.

The diff command compares file contents between two changes and shows what has
been added, removed, or modified.

Arguments:
- No arguments: Compare current change to its parent
- One argument: Compare specified change to current change
- Two arguments: Compare first change to second change

You can specify changes using:
- Full change name (e.g., "bitter-rose-1234")
- Change name prefix (e.g., "bitter-rose")
- Bookmark name (e.g., "main")

The output uses Git-style unified diff format, making it easy to see exactly
what changed between two versions.`,
		Example: `# Compare current change to its parent
pogo diff

# Compare current change to main bookmark
pogo diff main

# Compare two specific changes
pogo diff bitter-rose sweet-flower

# Compare using change prefixes
pogo diff bitter sweet`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 2 {
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

			if err := c.PushFull(false); err != nil {
				return errors.Join(errors.New("push before diff"), err)
			}

			var rev1, rev2 *string
			if len(args) >= 1 {
				rev1 = &args[0]
			}
			if len(args) >= 2 {
				rev2 = &args[1]
			}

			if err := c.Diff(rev1, rev2, cmd.OutOrStdout(), diffColorFlag); err != nil {
				return errors.Join(errors.New("diff"), err)
			}

			return nil
		},
	}
)

func init() {
	diffCmd.Flags().BoolVar(&diffColorFlag, "color", tty.IsInteractive(), "Enable colored output")
	RootCmd.AddCommand(diffCmd)
}
