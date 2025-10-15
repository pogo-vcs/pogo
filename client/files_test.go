package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/pogo-vcs/pogo/filecontents"
)

func TestIgnorePatternsWithDirectories(t *testing.T) {
	// Create a test directory structure
	testDir := t.TempDir()

	// Create a .gitignore file
	gitignoreContent := `node_modules/
*.log
dist/
`
	if err := os.WriteFile(filepath.Join(testDir, ".gitignore"), []byte(gitignoreContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create the directory structure that should be ignored
	nodeModulesDir := filepath.Join(testDir, "node_modules")
	if err := os.MkdirAll(nodeModulesDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a nested directory structure similar to the error
	deepDir := filepath.Join(nodeModulesDir, ".pnpm", "@ampproject+remapping@2.3.0", "node_modules", "@jridgewell", "gen-mapping")
	if err := os.MkdirAll(deepDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create some files in the ignored directory
	if err := os.WriteFile(filepath.Join(deepDir, "index.js"), []byte("console.log('test');"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a dist directory that should also be ignored
	distDir := filepath.Join(testDir, "dist")
	if err := os.MkdirAll(distDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "bundle.js"), []byte("// bundled"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create some files that should NOT be ignored
	if err := os.WriteFile(filepath.Join(testDir, "main.go"), []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(testDir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "app.js"), []byte("// app"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a client pointing to the test directory
	client := &Client{
		Location: testDir,
	}

	// Test the ignore matcher directly
	matcher, err := client.GetIgnoreMatcher()
	if err != nil {
		t.Fatal(err)
	}

	// Test if node_modules directory should be ignored
	if !matcher.Match([]string{"node_modules"}, true) {
		t.Error("node_modules directory should be ignored")
	}

	// Test if files inside node_modules should be ignored
	deepPath := strings.Split("node_modules/.pnpm/@ampproject+remapping@2.3.0/node_modules/@jridgewell/gen-mapping/index.js", "/")
	if !matcher.Match(deepPath, false) {
		t.Errorf("File in node_modules should be ignored: %v", deepPath)
	}

	// Collect all unignored files
	var collectedFiles []LocalFile
	for file := range client.UnignoredFiles {
		collectedFiles = append(collectedFiles, file)
	}

	// Verify that node_modules and dist are not in the collected files
	for _, file := range collectedFiles {
		if strings.Contains(file.Name, "node_modules") {
			t.Errorf("node_modules file should be ignored: %s", file.Name)
		}
		if strings.Contains(file.Name, "dist") {
			t.Errorf("dist file should be ignored: %s", file.Name)
		}
	}

	// Verify that main.go and src/app.js are included
	expectedFiles := map[string]bool{
		"main.go":    false,
		"src/app.js": false,
	}

	for _, file := range collectedFiles {
		if _, exists := expectedFiles[file.Name]; exists {
			expectedFiles[file.Name] = true
		}
	}

	for name, found := range expectedFiles {
		if !found {
			t.Errorf("Expected file %s was not collected", name)
		}
	}

	// Now test that hashing all collected files doesn't error
	for _, file := range collectedFiles {
		_, err := filecontents.HashFile(file.AbsPath)
		if err != nil {
			t.Errorf("Failed to hash file %s: %v", file.Name, err)
		}
	}
}

func TestGitIgnorePatternParsing(t *testing.T) {
	tests := []struct {
		pattern     string
		path        []string
		isDir       bool
		shouldMatch bool
	}{
		{"node_modules/", []string{"node_modules"}, true, true},
		{"node_modules/", []string{"node_modules", "package.json"}, false, true},
		{"node_modules/", []string{"node_modules", ".pnpm", "foo", "bar.js"}, false, true},
		{"dist/", []string{"dist"}, true, true},
		{"dist/", []string{"dist", "bundle.js"}, false, true},
		{"*.log", []string{"error.log"}, false, true},
		{"*.log", []string{"logs", "error.log"}, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+" "+strings.Join(tt.path, "/"), func(t *testing.T) {
			pattern := gitignore.ParsePattern(tt.pattern, nil)
			matcher := gitignore.NewMatcher([]gitignore.Pattern{pattern})
			if matcher.Match(tt.path, tt.isDir) != tt.shouldMatch {
				t.Errorf("Pattern %q with path %v (isDir=%v) should match=%v", tt.pattern, tt.path, tt.isDir, tt.shouldMatch)
			}
		})
	}
}