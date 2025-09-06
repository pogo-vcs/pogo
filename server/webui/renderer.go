package webui

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/a-h/templ"
	"github.com/niklasfasching/go-org/org"
	"github.com/pogo-vcs/pogo/filecontents"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"
)

// RenderFile renders a file based on its extension
func RenderFile(filename string, hash string) (string, error) {
	// Open the file using the filecontents abstraction
	filePath := filecontents.GetFilePathFromHash(hash)
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open %s hash %s, path %s: %w", filename, hash, filePath, err)
	}
	defer f.Close()

	// Read the file content
	content, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}

	// Get file extension
	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".md", ".markdown":
		return renderMarkdown(content)
	case ".org":
		orgStr, err := renderOrgMode(content)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("<h1>%s</h1>%s", filename, orgStr), nil
	default:
		// fall back to plain text
		return "<pre><code>" + html.EscapeString(string(content)) + "</code></pre>", nil
	}
}

// RenderFileComponent returns a templ component for rendering file content
func RenderFileComponent(filename string, hash string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		rendered, err := RenderFile(filename, hash)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, rendered)
		return err
	})
}

func renderMarkdown(content []byte) (string, error) {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			gmhtml.WithHardWraps(),
		),
	)

	var buf bytes.Buffer
	if err := md.Convert(content, &buf); err != nil {
		return "", err
	}

	return buf.String(), nil
}

var orgMode = org.New()

func renderOrgMode(content []byte) (string, error) {
	doc := orgMode.Parse(bytes.NewReader(content), "")
	if doc.Error != nil {
		return "", doc.Error
	}

	writer := org.NewHTMLWriter()
	str, err := doc.Write(writer)
	if err != nil {
		return "", err
	}

	return str, nil
}
