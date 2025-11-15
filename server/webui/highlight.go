package webui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"unicode"
	"unicode/utf8"

	"github.com/a-h/templ"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"

	"github.com/pogo-vcs/pogo/filecontents"
)

const maxHighlightedBytes = 512 * 1024 // 512 KiB

// ok reports whether a highlighted component was produced; false signals that callers should fall back to the raw object stream.
func HighlightedObjectComponent(ctx context.Context, filename, hash string) (templ.Component, bool, error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
	}

	reader, fileType, err := filecontents.OpenFileByHashWithType(hash)
	if err != nil {
		return nil, false, fmt.Errorf("open file type: %w", err)
	}
	defer reader.Close()

	if fileType.Binary {
		return nil, false, nil
	}

	highlightReader := fileType.CanonicalizeReader(reader)

	limited := &io.LimitedReader{R: highlightReader, N: maxHighlightedBytes + 1}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(limited); err != nil && !errors.Is(err, io.EOF) {
		return nil, false, fmt.Errorf("read file: %w", err)
	}

	if limited.N <= 0 {
		// File exceeds the limit; render raw content instead.
		return nil, false, nil
	}

	content := buf.Bytes()
	if len(content) == 0 {
		return templ.Raw("<pre><code></code></pre>"), true, nil
	}

	text := string(content)
	if !isHighlightableText(text) {
		return nil, false, nil
	}

	lexer := lexers.Match(filename)
	if lexer == nil {
		lexer = lexers.Analyse(text)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	iterator, err := lexer.Tokenise(nil, text)
	if err != nil {
		return nil, false, fmt.Errorf("tokenise: %w", err)
	}

	formatter := html.New(
		html.WithClasses(true),
		html.WithLineNumbers(true),
		html.LineNumbersInTable(false),
		html.TabWidth(4),
	)

	style := styles.Get("catppuccin-mocha")
	if style == nil {
		style = styles.Fallback
	}

	var highlighted bytes.Buffer
	if err := formatter.Format(&highlighted, style, iterator); err != nil {
		return nil, false, fmt.Errorf("format highlight: %w", err)
	}

	return templ.Raw(highlighted.String()), true, nil
}

func isHighlightableText(text string) bool {
	if len(text) == 0 {
		return true
	}

	if !utf8.ValidString(text) {
		return false
	}

	printable := 0
	total := 0
	for _, r := range text {
		total++
		if unicode.IsPrint(r) || unicode.IsSpace(r) {
			printable++
		}
	}

	if total == 0 {
		return false
	}

	return float64(printable)/float64(total) >= 0.85
}
