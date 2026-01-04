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
		// Use Lstat to detect symlinks without following them
		info, err := os.Lstat(absPath)
		if err != nil {
			return err
		}
		isDir := info.IsDir()
		isSymlink := info.Mode()&os.ModeSymlink != 0

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
		// Don't descend into symlinked directories
		if isDir {
			if isSymlink {
				// Treat symlinked directory as a file (track the symlink itself)
				if !yield(LocalFile{
					AbsPath: absPath,
					Name:    filepath.ToSlash(relPath),
				}) {
					return filepath.SkipAll
				}
				return filepath.SkipDir
			}
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

// IsSymlink checks if a path is a symlink and returns its target
func (c *Client) IsSymlink(absPath string) (bool, string, error) {
	info, err := os.Lstat(absPath)
	if err != nil {
		return false, "", fmt.Errorf("lstat file: %w", err)
	}

	if info.Mode()&os.ModeSymlink == 0 {
		return false, "", nil
	}

	target, err := os.Readlink(absPath)
	if err != nil {
		return true, "", fmt.Errorf("read symlink: %w", err)
	}

	return true, target, nil
}

// ValidateAndNormalizeSymlink validates a symlink target and normalizes it to a relative path
// Returns the normalized target path and an error if validation fails
func (c *Client) ValidateAndNormalizeSymlink(symlinkAbsPath string, target string) (string, error) {
	symlinkDir := filepath.Dir(symlinkAbsPath)

	var targetAbsPath string
	if filepath.IsAbs(target) {
		// Absolute symlink - try to convert to relative
		targetAbsPath = filepath.Clean(target)
	} else {
		// Relative symlink - resolve it
		targetAbsPath = filepath.Clean(filepath.Join(symlinkDir, target))
	}

	// Check if target is within repository
	relToRepo, err := filepath.Rel(c.Location, targetAbsPath)
	if err != nil {
		return "", fmt.Errorf("resolve target relative to repository: %w", err)
	}

	// Check if path escapes repository (contains ..)
	if strings.HasPrefix(relToRepo, "..") {
		return "", fmt.Errorf("symlink target points outside repository: %s", target)
	}

	// Convert to relative path from symlink to target
	relTarget, err := filepath.Rel(symlinkDir, targetAbsPath)
	if err != nil {
		return "", fmt.Errorf("convert to relative path: %w", err)
	}

	// Always use forward slashes for cross-platform compatibility
	normalizedTarget := filepath.ToSlash(relTarget)

	return normalizedTarget, nil
}

// GetSymlinkHash computes hash of the symlink target path (not the target's content)
func GetSymlinkHash(target string) []byte {
	h := sha256.New()
	h.Write([]byte(target))
	return h.Sum(nil)
}
