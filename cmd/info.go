package cmd

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"text/template"

	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/colors"
	"github.com/pogo-vcs/pogo/ptr"
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "Display the current working copy status",
	Long: `Display information about the current working copy and repository status.

This command is particularly useful for:

- Checking which change you're currently working on
- Seeing if there are any conflicts
- Integrating Pogo status into your shell prompt
- Scripting and automation

The output can be customized using Go's text/template syntax with the --format flag.

Available template variables:

| Variable                 | Description                                    |
| ------------------------ | ---------------------------------------------- |
` +
		"| `{{.ChangeNamePrefix}}`  | The adjective part of the change name          |\n" +
		"| `{{.ChangeNameSuffix}}`  | The noun and number part of the change name    |\n" +
		"| `{{.ChangeName}}`        | The full change name (prefix + suffix)         |\n" +
		"| `{{.ChangeDescription}}` | The description of the current change          |\n" +
		"| `{{.Bookmarks}}`         | Array of bookmarks pointing to this change     |\n" +
		"| `{{.IsInConflict}}`      | Boolean indicating if the change has conflicts |\n" +
		"| `{{.Error}}`             | Any error message (connection issues, etc.)    |\n" +
		`
The default format shows a colored prompt-friendly output with conflict
indicators and bookmark information.

Fish shell integration:
` + "\n```fish" + `
function fish_vcs_prompt --description 'Print all vcs prompts'
    pogo info $argv
    or fish_jj_prompt $argv
    or fish_git_prompt $argv
    or fish_hg_prompt $argv
    or fish_fossil_prompt $argv
end
` + "```",
	Example: `# Show default formatted info
pogo info

# Simple format showing just the change name
pogo info --format '{{.ChangeName}}'

# Format for shell prompt showing change and bookmarks
pogo info --format '({{.ChangeName}}{{range .Bookmarks}} [{{.}}]{{end}})'

# Show description if available
pogo info --format '{{.ChangeName}}: {{.ChangeDescription}}'

# Bash prompt integration example
export PS1='$(pogo info --format "{{.ChangeName}}") \$ '

# Check for conflicts in a script
pogo info --format '{{.IsInConflict}}'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		t, err := template.New("info format").Parse(cmd.Flag("format").Value.String())
		if err != nil {
			return fmt.Errorf("parse format template: %w", err)
		}

		wd, err := os.Getwd()
		if err != nil {
			printError(cmd.OutOrStdout(), t, err)
			return nil
		}

		if _, err := client.FindRepoFile(wd); err != nil {
			os.Exit(1)
			return nil
		}

		c, err := client.OpenFromFile(cmd.Context(), wd)
		if err != nil {
			printError(cmd.OutOrStdout(), t, err)
			return nil
		}
		defer c.Close()
		configureClientOutputs(cmd, c)

		infoResponse, err := c.Info()
		if err != nil {
			printError(cmd.OutOrStdout(), t, err)
			return nil
		}

		data := InfoData{
			ChangeNamePrefix:  infoResponse.ChangeNamePrefix,
			ChangeNameSuffix:  infoResponse.ChangeNameSuffix,
			ChangeName:        infoResponse.ChangeName,
			ChangeDescription: ptr.Or(infoResponse.ChangeDescription, ""),
			Bookmarks:         infoResponse.Bookmarks,
			IsInConflict:      infoResponse.IsInConflict,
		}

		if err = t.Execute(cmd.OutOrStdout(), data); err != nil {
			printError(cmd.OutOrStdout(), t, err)
			return nil
		}

		return nil
	},
}

func printError(w io.Writer, t *template.Template, err error) {
	defer os.Exit(1)

	errStr := err.Error()
	{
		errLines := strings.Split(errStr, "\n")
		for i, line := range errLines {
			errLines[i] = strings.TrimSpace(line)
		}
		errStr = strings.Join(errLines, " ")
	}

	// provide a simple and helpful error message for common errors
	if strings.Contains(errStr, "name resolver error") {
		errStr = "unable to resolve host name"
	} else if matches := regexp.MustCompile(`dial\s+tcp\s+([^\s]+):\s+connect:\s+connection\s+refused`).
		FindStringSubmatch(errStr); len(matches) > 1 {
		errStr = fmt.Sprintf("host %s refused connection", matches[1])
	}

	if tErr := t.Execute(w, InfoData{
		Error: errStr,
	}); tErr != nil {
		_, _ = fmt.Fprintln(w, err.Error())
	}
}

type InfoData struct {
	ChangeNamePrefix  string
	ChangeNameSuffix  string
	ChangeName        string
	ChangeDescription string
	Bookmarks         []string
	IsInConflict      bool
	Error             string
}

func init() {
	defaultFormat := fmt.Sprintf(
		"({{if .Error}}%s{{.Error}}%s{{else}}{{if .IsInConflict}}💥{{end}}%s{{.ChangeNamePrefix}}%s{{.ChangeNameSuffix}}%s {{- range $i, $b := .Bookmarks}}{{if $i}}, {{else}} {{end}}{{if eq . \"main\"}}%s{{.}}%s{{else}}{{.}}{{end}}{{end}}{{end}})",
		colors.Red,
		colors.Reset,
		colors.Magenta,
		colors.BrightBlack,
		colors.Reset,
		colors.Green,
		colors.Reset,
	)
	infoCmd.Flags().String(
		"format",
		defaultFormat,
		"Format string for the prompt output",
	)
	RootCmd.AddCommand(infoCmd)
}
