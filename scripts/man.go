package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/pogo-vcs/pogo/cmd"
	"github.com/spf13/cobra/doc"
)

func Man(manDir string) error {
	fmt.Printf("Generating manpages into %s\n", manDir)

	if err := os.MkdirAll(manDir, 0755); err != nil {
		return err
	}

	cmd.RootCmd.InitDefaultCompletionCmd()
	cmd.RootCmd.InitDefaultHelpCmd()

	hdr := &doc.GenManHeader{
		Title:   cmd.RootCmd.Name(),
		Section: "1",
		Manual:  "Pogo Manual",
		Source:  fmt.Sprintf("%s/%s", cmd.RootCmd.Name(), cmd.Version),
	}
	if err := doc.GenManTree(cmd.RootCmd, hdr, manDir); err != nil {
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
}