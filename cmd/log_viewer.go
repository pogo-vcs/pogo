package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type logViewModel struct {
	viewport viewport.Model
	content  string
	ready    bool
}

func initialLogModel(content string) logViewModel {
	return logViewModel{
		content: content,
	}
}

func (m logViewModel) Init() tea.Cmd {
	return nil
}

func (m logViewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		headerHeight := 0
		footerHeight := 1
		verticalMarginHeight := headerHeight + footerHeight

		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-verticalMarginHeight)
			m.viewport.YPosition = 0
			m.viewport.SetContent(m.content)
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - verticalMarginHeight
		}
	}

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m logViewModel) View() string {
	if !m.ready {
		return "\n  Initializing..."
	}

	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	footer := footerStyle.Render(m.helpView())

	return m.viewport.View() + "\n" + footer
}

func (m logViewModel) helpView() string {
	var help []string

	if m.viewport.AtTop() {
		help = append(help, "↓/j: down")
	} else if m.viewport.AtBottom() {
		help = append(help, "↑/k: up")
	} else {
		help = append(help, "↑/k: up", "↓/j: down")
	}

	help = append(help, "PgUp/PgDn: page", "Home/End: top/bottom", "q/Esc: quit")

	scrollInfo := ""
	if m.viewport.TotalLineCount() > 0 {
		scrollPercent := int(float64(m.viewport.YOffset+m.viewport.Height) / float64(m.viewport.TotalLineCount()) * 100)
		if scrollPercent > 100 {
			scrollPercent = 100
		}
		scrollInfo = " " + strings.Repeat("─", 3) + " " + lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(
			fmt.Sprintf("(%d%%)", scrollPercent),
		)
	}

	return strings.Join(help, " • ") + scrollInfo
}
