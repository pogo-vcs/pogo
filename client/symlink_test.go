package client

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsSymlink(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "pogo-symlink-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	c := &Client{Location: tmpDir}

	// Create a regular file
	regularFile := filepath.Join(tmpDir, "regular.txt")
	if err := os.WriteFile(regularFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink
	symlinkPath := filepath.Join(tmpDir, "link.txt")
	if err := os.Symlink("regular.txt", symlinkPath); err != nil {
		t.Skip("Symlink creation not supported on this system:", err)
	}

	// Test regular file
	isSymlink, target, err := c.IsSymlink(regularFile)
	if err != nil {
		t.Errorf("IsSymlink(regular file) error: %v", err)
	}
	if isSymlink {
		t.Error("IsSymlink(regular file) should return false")
	}
	if target != "" {
		t.Errorf("IsSymlink(regular file) should return empty target, got %q", target)
	}

	// Test symlink
	isSymlink, target, err = c.IsSymlink(symlinkPath)
	if err != nil {
		t.Errorf("IsSymlink(symlink) error: %v", err)
	}
	if !isSymlink {
		t.Error("IsSymlink(symlink) should return true")
	}
	if target != "regular.txt" {
		t.Errorf("IsSymlink(symlink) target = %q, want %q", target, "regular.txt")
	}
}

func TestValidateAndNormalizeSymlink(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "pogo-symlink-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create subdirectory
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	c := &Client{Location: tmpDir}

	tests := []struct {
		name          string
		symlinkPath   string
		target        string
		wantNormalized string
		wantError     bool
	}{
		{
			name:          "relative path within repo",
			symlinkPath:   filepath.Join(subDir, "link.txt"),
			target:        "../file.txt",
			wantNormalized: "../file.txt",
			wantError:     false,
		},
		{
			name:          "relative path to same dir",
			symlinkPath:   filepath.Join(subDir, "link.txt"),
			target:        "file.txt",
			wantNormalized: "file.txt",
			wantError:     false,
		},
		{
			name:        "path outside repo",
			symlinkPath: filepath.Join(subDir, "link.txt"),
			target:      "../../outside.txt",
			wantError:   true,
		},
		{
			name:          "absolute path within repo",
			symlinkPath:   filepath.Join(subDir, "link.txt"),
			target:        filepath.Join(tmpDir, "file.txt"),
			wantNormalized: "../file.txt",
			wantError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized, err := c.ValidateAndNormalizeSymlink(tt.symlinkPath, tt.target)
			if tt.wantError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if normalized != tt.wantNormalized {
					t.Errorf("Normalized = %q, want %q", normalized, tt.wantNormalized)
				}
			}
		})
	}
}

func TestGetSymlinkHash(t *testing.T) {
	target1 := "path/to/file.txt"
	target2 := "path/to/other.txt"

	hash1 := GetSymlinkHash(target1)
	hash2 := GetSymlinkHash(target2)

	// Same target should produce same hash
	hash1Again := GetSymlinkHash(target1)
	if string(hash1) != string(hash1Again) {
		t.Error("Same target should produce same hash")
	}

	// Different targets should produce different hashes
	if string(hash1) == string(hash2) {
		t.Error("Different targets should produce different hashes")
	}

	// Hash should be 32 bytes (SHA-256)
	if len(hash1) != 32 {
		t.Errorf("Hash length = %d, want 32", len(hash1))
	}
}
