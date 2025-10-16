package difftui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pogo-vcs/pogo/protos"
)

type DiffFile struct {
	Header *protos.DiffFileHeader
	Blocks []*protos.DiffBlock
}

type DiffData struct {
	Files []DiffFile
}

type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	NextFile key.Binding
	PrevFile key.Binding
	Top      key.Binding
	Bottom   key.Binding
	Quit     key.Binding
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("k", "up"),
		key.WithHelp("k/↑", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("j", "down"),
		key.WithHelp("j/↓", "down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("ctrl+u", "pgup"),
		key.WithHelp("ctrl+u", "page up"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("ctrl+d", "pgdown"),
		key.WithHelp("ctrl+d", "page down"),
	),
	NextFile: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "next file"),
	),
	PrevFile: key.NewBinding(
		key.WithKeys("p"),
		key.WithHelp("p", "prev file"),
	),
	Top: key.NewBinding(
		key.WithKeys("g"),
		key.WithHelp("g", "top"),
	),
	Bottom: key.NewBinding(
		key.WithKeys("G"),
		key.WithHelp("G", "bottom"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}

var (
	statusStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#64b5f6")).Background(lipgloss.Color("#1e1e1e"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#90a4ae")).Background(lipgloss.Color("#1e1e1e"))
)

type model struct {
	data         DiffData
	currentFile  int
	viewport     viewport.Model
	ready        bool
	renderedDiff string
	width        int
}

func NewModel(data DiffData) model {
	return model{
		data:        data,
		currentFile: 0,
		viewport:    viewport.New(80, 24),
		ready:       false,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, keys.NextFile):
			if m.currentFile < len(m.data.Files)-1 {
				m.currentFile++
				m.renderCurrentFile()
				m.viewport.SetContent(m.renderedDiff)
				m.viewport.GotoTop()
			}
			return m, nil

		case key.Matches(msg, keys.PrevFile):
			if m.currentFile > 0 {
				m.currentFile--
				m.renderCurrentFile()
				m.viewport.SetContent(m.renderedDiff)
				m.viewport.GotoTop()
			}
			return m, nil

		case key.Matches(msg, keys.Top):
			m.viewport.GotoTop()
			return m, nil

		case key.Matches(msg, keys.Bottom):
			m.viewport.GotoBottom()
			return m, nil

		case key.Matches(msg, keys.Up):
			m.viewport.LineUp(1)
			return m, nil

		case key.Matches(msg, keys.Down):
			m.viewport.LineDown(1)
			return m, nil

		case key.Matches(msg, keys.PageUp):
			m.viewport.ViewUp()
			return m, nil

		case key.Matches(msg, keys.PageDown):
			m.viewport.ViewDown()
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		headerHeight := 2
		footerHeight := 1
		verticalMargins := headerHeight + footerHeight

		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-verticalMargins)
			if len(m.data.Files) > 0 {
				m.renderCurrentFile()
				m.viewport.SetContent(m.renderedDiff)
			}
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - verticalMargins
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	if len(m.data.Files) == 0 {
		return "No changes to display.\nPress q to quit."
	}

	file := m.data.Files[m.currentFile]
	status := fmt.Sprintf("File %d/%d: %s", m.currentFile+1, len(m.data.Files), file.Header.Path)
	help := "j/k: scroll | ctrl+u/d: page | n/p: next/prev file | g/G: top/bottom | q: quit"

	return fmt.Sprintf(
		"%s\n%s\n%s",
		statusStyle.Render(status),
		m.viewport.View(),
		helpStyle.Render(help),
	)
}

func getLexerForFile(path string) chroma.Lexer {
	lexer := lexers.Match(filepath.Base(path))
	if lexer == nil {
		lexer = lexers.Fallback
	}
	return chroma.Coalesce(lexer)
}

var ansiColorMap = map[chroma.TokenType]string{
	chroma.Keyword:           "\x1b[38;2;249;38;114m",
	chroma.KeywordNamespace:  "\x1b[38;2;249;38;114m",
	chroma.KeywordType:       "\x1b[38;2;102;217;239m",
	chroma.Name:              "\x1b[38;2;248;248;242m",
	chroma.NameClass:         "\x1b[38;2;166;226;46m",
	chroma.NameFunction:      "\x1b[38;2;166;226;46m",
	chroma.NameBuiltin:       "\x1b[38;2;102;217;239m",
	chroma.LiteralString:     "\x1b[38;2;230;219;116m",
	chroma.LiteralNumber:     "\x1b[38;2;174;129;255m",
	chroma.Operator:          "\x1b[38;2;249;38;114m",
	chroma.Comment:           "\x1b[38;2;117;113;94m",
	chroma.CommentSingle:     "\x1b[38;2;117;113;94m",
	chroma.CommentMultiline:  "\x1b[38;2;117;113;94m",
	chroma.CommentPreproc:    "\x1b[38;2;102;217;239m",
	chroma.LiteralStringDoc:  "\x1b[38;2;117;113;94m",
	chroma.Generic:           "\x1b[38;2;248;248;242m",
	chroma.GenericHeading:    "\x1b[1m",
	chroma.GenericSubheading: "\x1b[1m",
	chroma.GenericEmph:       "\x1b[3m",
	chroma.GenericStrong:     "\x1b[1m",
}

func padLineWithBg(line string, width int) string {
	line = strings.ReplaceAll(line, "\t", "    ")

	visibleLen := 0
	inEscape := false

	for _, r := range line {
		if r == '\x1b' {
			inEscape = true
		} else if inEscape && r == 'm' {
			inEscape = false
		} else if !inEscape {
			visibleLen++
		}
	}

	if visibleLen < width {
		padding := strings.Repeat(" ", width-visibleLen)
		return line + padding + "\x1b[0m"
	}
	return line + "\x1b[0m"
}

func highlightLine(line string, lexer chroma.Lexer) string {
	iterator, err := lexer.Tokenise(nil, line)
	if err != nil {
		return line
	}

	var b strings.Builder
	defaultFg := "\x1b[38;2;224;224;224m"

	for token := iterator(); token != chroma.EOF; token = iterator() {
		tokenType := token.Type
		for tokenType != chroma.Background {
			if color, ok := ansiColorMap[tokenType]; ok {
				b.WriteString(color)
				b.WriteString(token.Value)
				b.WriteString(defaultFg)
				goto nextToken
			}
			if tokenType == chroma.Text || tokenType == 0 {
				break
			}
			tokenType = tokenType.Parent()
		}
		b.WriteString(token.Value)
	nextToken:
	}

	return b.String()
}

func (m *model) renderCurrentFile() {
	if len(m.data.Files) == 0 {
		m.renderedDiff = "No files to display"
		return
	}

	if m.currentFile >= len(m.data.Files) {
		m.renderedDiff = fmt.Sprintf("Error: invalid file index %d/%d", m.currentFile, len(m.data.Files))
		return
	}

	file := m.data.Files[m.currentFile]
	var b strings.Builder

	headerLine := fmt.Sprintf("\x1b[48;2;30;30;30m\x1b[38;2;144;164;174m\x1b[1mdiff --git a/%s b/%s", file.Header.Path, file.Header.Path)
	b.WriteString(padLineWithBg(headerLine, m.width) + "\n")

	switch file.Header.Status {
	case protos.DiffFileStatus_DIFF_FILE_STATUS_ADDED:
		b.WriteString(padLineWithBg("\x1b[48;2;30;30;30m\x1b[38;2;120;144;156mnew file mode 100644", m.width) + "\n")
		b.WriteString(padLineWithBg("\x1b[48;2;30;30;30m\x1b[38;2;120;144;156m--- /dev/null", m.width) + "\n")
		b.WriteString(padLineWithBg(fmt.Sprintf("\x1b[48;2;30;30;30m\x1b[38;2;120;144;156m+++ b/%s", file.Header.Path), m.width) + "\n")
		b.WriteString(padLineWithBg(fmt.Sprintf("\x1b[48;2;30;30;30m\x1b[38;2;120;144;156m@@ -0,0 +1,%d @@", file.Header.NewLineCount), m.width) + "\n")

	case protos.DiffFileStatus_DIFF_FILE_STATUS_DELETED:
		b.WriteString(padLineWithBg("\x1b[48;2;30;30;30m\x1b[38;2;120;144;156mdeleted file mode 100644", m.width) + "\n")
		b.WriteString(padLineWithBg(fmt.Sprintf("\x1b[48;2;30;30;30m\x1b[38;2;120;144;156m--- a/%s", file.Header.Path), m.width) + "\n")
		b.WriteString(padLineWithBg("\x1b[48;2;30;30;30m\x1b[38;2;120;144;156m+++ /dev/null", m.width) + "\n")
		b.WriteString(padLineWithBg(fmt.Sprintf("\x1b[48;2;30;30;30m\x1b[38;2;120;144;156m@@ -1,%d +0,0 @@", file.Header.OldLineCount), m.width) + "\n")

	case protos.DiffFileStatus_DIFF_FILE_STATUS_BINARY:
		b.WriteString(padLineWithBg(fmt.Sprintf("\x1b[48;2;30;30;30m\x1b[38;2;120;144;156mindex %s..%s", file.Header.OldHash, file.Header.NewHash), m.width) + "\n")
		b.WriteString(padLineWithBg("\x1b[48;2;30;30;30m\x1b[38;2;120;144;156mBinary file", m.width) + "\n")

	case protos.DiffFileStatus_DIFF_FILE_STATUS_MODIFIED:
		b.WriteString(padLineWithBg(fmt.Sprintf("\x1b[48;2;30;30;30m\x1b[38;2;120;144;156mindex %s..%s", file.Header.OldHash, file.Header.NewHash), m.width) + "\n")
		b.WriteString(padLineWithBg(fmt.Sprintf("\x1b[48;2;30;30;30m\x1b[38;2;120;144;156m--- a/%s", file.Header.Path), m.width) + "\n")
		b.WriteString(padLineWithBg(fmt.Sprintf("\x1b[48;2;30;30;30m\x1b[38;2;120;144;156m+++ b/%s", file.Header.Path), m.width) + "\n")
	}

	if len(file.Blocks) == 0 {
		b.WriteString("\n(No diff blocks)\n")
	}

	lexer := getLexerForFile(file.Header.Path)

	for _, block := range file.Blocks {
		switch block.Type {
		case protos.DiffBlockType_DIFF_BLOCK_TYPE_METADATA:
			for _, line := range block.Lines {
				rendered := "\x1b[48;2;30;30;30m\x1b[38;2;120;144;156m" + line
				padded := padLineWithBg(rendered, m.width)
				b.WriteString(padded + "\n")
			}

		case protos.DiffBlockType_DIFF_BLOCK_TYPE_UNCHANGED:
			for _, line := range block.Lines {
				highlighted := highlightLine(line, lexer)
				fullLine := "\x1b[48;2;30;30;30m\x1b[38;2;224;224;224m " + highlighted
				padded := padLineWithBg(fullLine, m.width)
				b.WriteString(padded + "\n")
			}

		case protos.DiffBlockType_DIFF_BLOCK_TYPE_REMOVED:
			for _, line := range block.Lines {
				highlighted := highlightLine(line, lexer)
				fullLine := "\x1b[48;2;100;30;30m\x1b[38;2;255;235;238m-" + highlighted
				padded := padLineWithBg(fullLine, m.width)
				b.WriteString(padded + "\n")
			}

		case protos.DiffBlockType_DIFF_BLOCK_TYPE_ADDED:
			for _, line := range block.Lines {
				highlighted := highlightLine(line, lexer)
				fullLine := "\x1b[48;2;30;80;30m\x1b[38;2;232;245;233m+" + highlighted
				padded := padLineWithBg(fullLine, m.width)
				b.WriteString(padded + "\n")
			}
		}
	}

	m.renderedDiff = b.String()
}

func Run(data DiffData) error {
	m := NewModel(data)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}
