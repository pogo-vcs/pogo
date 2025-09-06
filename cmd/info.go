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
	Use:     "info",
	Example: `pogo info --format '({{.ChangeName}} {{- range $i, $b := .Bookmarks}}{{- if $i}}, {{else}} {{end}}{{.}}{{- end}})'`,
	Short:   "Display the current working copy status",
	Long: `Use this command to render the pogo information to things like a shell prompt.
The format can be customized with the --format flag, which uses Go's text/template package.
Available variables are:

{{.ChangeNamePrefix}} - The change name prefix
{{.ChangeNameSuffix}} - The change name suffix
{{.ChangeName}} - The change name
{{.ChangeDescription}} - The change description
{{.Bookmarks}} - The bookmarks for the change
{{.IsInConflict}} - Whether the change is in conflict
{{.Error}} - Any error that occurred
`,
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
	rootCmd.AddCommand(infoCmd)
}
