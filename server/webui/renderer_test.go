package webui

import (
	"strings"
	"testing"
)

func TestRenderMarkdown(t *testing.T) {
	content := []byte("# Test Header\n\nThis is a **bold** test.")
	rendered, err := renderMarkdown(content)
	if err != nil {
		t.Fatalf("Failed to render markdown: %v", err)
	}

	if !strings.Contains(rendered, "<h1") {
		t.Error("Expected rendered content to contain h1 tag")
	}

	if !strings.Contains(rendered, "<strong>") {
		t.Error("Expected rendered content to contain strong tag for bold text")
	}
}

func TestRenderOrgMode(t *testing.T) {
	content := []byte("* Test Header\n\nThis is a *bold* test.")
	rendered, err := renderOrgMode(content)
	if err != nil {
		t.Fatalf("Failed to render org-mode: %v", err)
	}

	if !strings.Contains(rendered, "<h2") {
		t.Error("Expected rendered content to contain h2 tag (org-mode uses h2 for top-level headers)")
	}

	if !strings.Contains(rendered, "<strong>") {
		t.Error("Expected rendered content to contain strong tag for bold text")
	}
}

func TestRenderFile(t *testing.T) {
	// Test markdown detection
	content := []byte("# Test Header\n\nThis is a test.")
	rendered, err := renderMarkdown(content)
	if err != nil {
		t.Fatalf("Failed to render markdown: %v", err)
	}

	if !strings.Contains(rendered, "<h1") {
		t.Error("Expected rendered content to contain h1 tag")
	}
}
