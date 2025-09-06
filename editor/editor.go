package editor

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/leodido/go-conventionalcommits"
	"github.com/leodido/go-conventionalcommits/parser"
	"github.com/pogo-vcs/pogo/tty"
)

func Edit(title string, value string) (string, error) {
	if !tty.IsInteractive() {
		return value, errors.New("cannot open interactive editor in non-interactive shell")
	}
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewText().
				Value(&value).
				ExternalEditor(true).
				ShowLineNumbers(true).
				Title(title),
		),
	).Run()
	if err != nil {
		return "", err
	}
	return value, nil
}

var ccParser = parser.NewMachine(conventionalcommits.WithTypes(conventionalcommits.TypesConventional))

func ConventionalCommit(currentDescription string) (string, error) {
	res, err := ccParser.Parse([]byte(currentDescription))
	if err != nil && len(currentDescription) > 0 {
		return Edit("Change description", currentDescription)
	}
	if res == nil {
		res = &conventionalcommits.ConventionalCommit{}
	}
	cc, ok := res.(*conventionalcommits.ConventionalCommit)
	if !ok {
		return "", fmt.Errorf("unexpected commit message type: %T", res)
	}

	if cc.Scope == nil {
		cc.Scope = new(string)
	}
	if cc.Body == nil {
		cc.Body = new(string)
	}
	if cc.Footers == nil {
		cc.Footers = map[string][]string{}
	}

	footerStr := footerString(cc.Footers)

	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Value(&cc.Type).
			Title("Type").
			Description("feat/fix/etc.").
			CharLimit(32).
			Suggestions([]string{"fix", "feat", "build", "chore", "ci", "docs", "style", "refactor", "perf", "test"}).
			Validate(huh.ValidateNotEmpty()),
		huh.NewInput().
			Value(cc.Scope).
			Title("Scope").
			Description("What part of the code is affected? (optional)"),
		huh.NewConfirm().
			Value(&cc.Exclamation).
			Title("Breaking changes?"),
		huh.NewInput().
			Value(&cc.Description).
			Title("Description").
			Placeholder("Description of the change").
			CharLimit(72).
			Validate(huh.ValidateNotEmpty()),
		huh.NewText().
			Value(cc.Body).
			Title("Body"),
		huh.NewText().
			Value(&footerStr).
			Title("Footers"),
	)).Run(); err != nil {
		return "", err
	}

	var ccSB strings.Builder
	ccSB.WriteString(cc.Type)
	if cc.Scope != nil && len(*cc.Scope) > 0 {
		ccSB.WriteString("(")
		ccSB.WriteString(*cc.Scope)
		ccSB.WriteString(")")
	}
	if cc.Exclamation {
		ccSB.WriteString("!")
	}
	ccSB.WriteString(": ")
	ccSB.WriteString(cc.Description)
	if cc.Body != nil && len(*cc.Body) > 0 {
		ccSB.WriteString("\n\n")
		ccSB.WriteString(*cc.Body)
	}
	if len(footerStr) > 0 {
		ccSB.WriteString("\n\n")
		ccSB.WriteString(footerStr)
	}

	return ccSB.String(), nil
}

func footerString(footers map[string][]string) string {
	var sb strings.Builder

	i := 0
	for k, v := range footers {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("%s: %s", k, strings.Join(v, ", ")))
		i++
	}

	return sb.String()
}
