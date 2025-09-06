package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

var manCmd = &cobra.Command{
	Use:    "gen-man",
	Short:  "Generate man pages",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		hdr := &doc.GenManHeader{
			Title:   rootCmd.Name(),
			Section: "1",
			Manual:  "Pogo Manual",
			Source:  fmt.Sprintf("%s/%s", rootCmd.Name(), Version),
		}
		manDir := filepath.Join("docs", "man")
		if err := os.MkdirAll(manDir, 0755); err != nil {
			return err
		}
		if err := doc.GenManTree(rootCmd, hdr, manDir); err != nil {
			return err
		}
		manDirEntries, err := os.ReadDir(manDir)
		if err != nil {
			return err
		}

		var newManpagesStr strings.Builder
		newManpagesStr.WriteString("    manpages:")
		for _, entry := range manDirEntries {
			if entry.IsDir() {
				continue
			}
			newManpagesStr.WriteString("\n      - docs/man/" + entry.Name())
		}

		grc, _ := os.ReadFile(".goreleaser.yaml")
		grcStr := string(grc)
		manpageRe := regexp.MustCompile(`    manpages:(?:\n +- \S+)+`)
		grcStr = manpageRe.ReplaceAllString(grcStr, newManpagesStr.String())

		if err := os.WriteFile(".goreleaser.yaml", []byte(grcStr), 0644); err != nil {
			return err
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(manCmd)
}
