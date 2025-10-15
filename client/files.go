package client

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

type LocalFile struct {
	AbsPath string
	Name    string
}

func (f LocalFile) Open() (*os.File, error) {
	return os.Open(f.AbsPath)
}

func (c *Client) UnignoredFiles(yield func(LocalFile) bool) {
	m, err := c.GetIgnoreMatcher()
	if err != nil {
		return
	}
	_ = filepath.WalkDir(c.Location, func(absPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		isDir := d.IsDir()
		relPath, err := filepath.Rel(c.Location, absPath)
		if err != nil {
			return fmt.Errorf("get relative path of %s to %s: %w", absPath, c.Location, err)
		}
		// gitignore expects forward slashes, even on Windows
		gitPath := strings.Split(filepath.ToSlash(relPath), "/")
		if m.Match(gitPath, isDir) {
			if isDir {
				return filepath.SkipDir
			}
			return nil
		}
		if isDir {
			return nil
		}
		if !yield(LocalFile{
			AbsPath: absPath,
			Name:    filepath.ToSlash(relPath),
		}) {
			return filepath.SkipAll
		}
		return nil
	})
}

var defaultIgnorePatterns = []gitignore.Pattern{
	gitignore.ParsePattern(".git", nil),
	gitignore.ParsePattern(".DS_Store", nil),
	gitignore.ParsePattern("Thumbs.db", nil),
	gitignore.ParsePattern(".pogo.yaml", nil),
}

func (c *Client) GetIgnoreMatcher() (gitignore.Matcher, error) {
	var patterns []gitignore.Pattern
	patterns = append(patterns, defaultIgnorePatterns...)
	for absPath := range c.AllIgnoreFiles {
		f, err := os.Open(absPath)
		if err != nil {
			return nil, fmt.Errorf("open ignore file: %w", err)
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			s := strings.TrimSpace(scanner.Text())
			if len(s) == 0 || strings.HasPrefix(s, "#") {
				continue
			}
			relDir, err := filepath.Rel(c.Location, filepath.Dir(absPath))
			if err != nil {
				return nil, fmt.Errorf("get relative path of %s to %s: %w", absPath, c.Location, err)
			}
			var domain []string
			if relDir != "." {
				// Use forward slashes for gitignore domains
				domain = strings.Split(filepath.ToSlash(relDir), "/")
			}
			patterns = append(patterns, gitignore.ParsePattern(s, domain))
		}
	}
	return gitignore.NewMatcher(patterns), nil
}

func (c *Client) AllIgnoreFiles(yield func(string) bool) {
	_ = filepath.WalkDir(c.Location, func(absPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() != ".gitignore" && d.Name() != ".pogoignore" {
			return nil
		}
		if !yield(absPath) {
			return filepath.SkipAll
		}
		return nil
	})
}

func GetContentHash(absPath string) ([]byte, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, fmt.Errorf("copy file for hash: %w", err)
	}

	return h.Sum(nil), nil
}