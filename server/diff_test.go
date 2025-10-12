//go:build fakekeyring

package server_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/db"
	"github.com/pogo-vcs/pogo/filecontents"
	"github.com/pogo-vcs/pogo/protos"
	"github.com/pogo-vcs/pogo/server"
)

func TestResolveDiff_NoChanges(t *testing.T) {
	env := setupTestEnvironment(t, "")
	defer env.cleanup()

	ctx := context.Background()

	repoPath, err := os.MkdirTemp("", "pogo-test-repo-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(repoPath)

	repoId, _, err := initializeRepository(ctx, repoPath, "test-no-changes", env.serverAddr)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	c, err := client.OpenFromFile(ctx, repoPath)
	if err != nil {
		t.Fatalf("Failed to open client: %v", err)
	}
	defer c.Close()

	if err := os.WriteFile(filepath.Join(c.Location, "test.txt"), []byte("Hello, World!\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	if err := c.PushFull(false); err != nil {
		t.Fatalf("Failed to push: %v", err)
	}

	_, changeName1, err := c.NewChange(nil, nil)
	if err != nil {
		t.Fatalf("Failed to create new change: %v", err)
	}

	if err := c.Edit(changeName1); err != nil {
		t.Fatalf("Failed to edit change: %v", err)
	}

	if err := c.PushFull(false); err != nil {
		t.Fatalf("Failed to push: %v", err)
	}

	change1Id, err := db.Q.FindChangeByNameFuzzy(ctx, repoId, changeName1)
	if err != nil {
		t.Fatalf("Failed to find change: %v", err)
	}

	parents, err := db.Q.GetChangeParents(ctx, change1Id)
	if err != nil {
		t.Fatalf("Failed to get parents: %v", err)
	}
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	parent0Name := parents[0].Name

	diffs, err := server.ResolveDiff(ctx, repoId, &parent0Name, &changeName1, nil)
	if err != nil {
		t.Fatalf("ResolveDiff failed: %v", err)
	}

	if len(diffs) != 0 {
		t.Errorf("Expected no diffs, got %d", len(diffs))
	}
}

func TestResolveDiff_UTF8TextModified(t *testing.T) {
	env := setupTestEnvironment(t, "")
	defer env.cleanup()

	ctx := context.Background()

	repoPath, err := os.MkdirTemp("", "pogo-test-repo-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(repoPath)

	repoId, _, err := initializeRepository(ctx, repoPath, "test-utf8-modified", env.serverAddr)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	c, err := client.OpenFromFile(ctx, repoPath)
	if err != nil {
		t.Fatalf("Failed to open client: %v", err)
	}
	defer c.Close()

	if err := os.WriteFile(filepath.Join(c.Location, "test.txt"), []byte("Hello, World!\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	if err := c.PushFull(false); err != nil {
		t.Fatalf("Failed to push: %v", err)
	}

	_, changeName1, err := c.NewChange(nil, nil)
	if err != nil {
		t.Fatalf("Failed to create new change: %v", err)
	}

	if err := c.Edit(changeName1); err != nil {
		t.Fatalf("Failed to edit change: %v", err)
	}

	if err := os.WriteFile(filepath.Join(c.Location, "test.txt"), []byte("Hello, Pogo!\n"), 0644); err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	if err := c.PushFull(false); err != nil {
		t.Fatalf("Failed to push: %v", err)
	}

	change1Id, err := db.Q.FindChangeByNameFuzzy(ctx, repoId, changeName1)
	if err != nil {
		t.Fatalf("Failed to find change: %v", err)
	}

	parents, err := db.Q.GetChangeParents(ctx, change1Id)
	if err != nil {
		t.Fatalf("Failed to get parents: %v", err)
	}
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	parent0Name := parents[0].Name

	diffs, err := server.ResolveDiff(ctx, repoId, &parent0Name, &changeName1, nil)
	if err != nil {
		t.Fatalf("ResolveDiff failed: %v", err)
	}

	if len(diffs) != 1 {
		t.Fatalf("Expected 1 diff, got %d", len(diffs))
	}

	diff := diffs[0]
	if diff.Path != "test.txt" {
		t.Errorf("Expected path 'test.txt', got '%s'", diff.Path)
	}
	if diff.Status != protos.DiffFileStatus_DIFF_FILE_STATUS_MODIFIED {
		t.Errorf("Expected status MODIFIED, got %v", diff.Status)
	}

	oldHash := base64.URLEncoding.EncodeToString(diff.OldContentHash)
	isBinary, err := isBinaryFileTest(oldHash)
	if err != nil {
		t.Fatalf("Failed to check if old file is binary: %v", err)
	}
	if isBinary {
		t.Error("Old file should not be detected as binary")
	}

	newHash := base64.URLEncoding.EncodeToString(diff.NewContentHash)
	isBinary, err = isBinaryFileTest(newHash)
	if err != nil {
		t.Fatalf("Failed to check if new file is binary: %v", err)
	}
	if isBinary {
		t.Error("New file should not be detected as binary")
	}
}

func TestResolveDiff_UTF16LETextModified(t *testing.T) {
	env := setupTestEnvironment(t, "")
	defer env.cleanup()

	ctx := context.Background()

	repoPath, err := os.MkdirTemp("", "pogo-test-repo-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(repoPath)

	repoId, _, err := initializeRepository(ctx, repoPath, "test-utf16le-modified", env.serverAddr)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	c, err := client.OpenFromFile(ctx, repoPath)
	if err != nil {
		t.Fatalf("Failed to open client: %v", err)
	}
	defer c.Close()

	utf16leContent1 := []byte{0xFF, 0xFE, 'H', 0, 'e', 0, 'l', 0, 'l', 0, 'o', 0, '\n', 0}
	if err := os.WriteFile(filepath.Join(c.Location, "test.txt"), utf16leContent1, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	if err := c.PushFull(false); err != nil {
		t.Fatalf("Failed to push: %v", err)
	}

	_, changeName1, err := c.NewChange(nil, nil)
	if err != nil {
		t.Fatalf("Failed to create new change: %v", err)
	}

	if err := c.Edit(changeName1); err != nil {
		t.Fatalf("Failed to edit change: %v", err)
	}

	utf16leContent2 := []byte{0xFF, 0xFE, 'P', 0, 'o', 0, 'g', 0, 'o', 0, '\n', 0}
	if err := os.WriteFile(filepath.Join(c.Location, "test.txt"), utf16leContent2, 0644); err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	if err := c.PushFull(false); err != nil {
		t.Fatalf("Failed to push: %v", err)
	}

	change1Id, err := db.Q.FindChangeByNameFuzzy(ctx, repoId, changeName1)
	if err != nil {
		t.Fatalf("Failed to find change: %v", err)
	}

	parents, err := db.Q.GetChangeParents(ctx, change1Id)
	if err != nil {
		t.Fatalf("Failed to get parents: %v", err)
	}
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	parent0Name := parents[0].Name

	diffs, err := server.ResolveDiff(ctx, repoId, &parent0Name, &changeName1, nil)
	if err != nil {
		t.Fatalf("ResolveDiff failed: %v", err)
	}

	if len(diffs) != 1 {
		t.Fatalf("Expected 1 diff, got %d", len(diffs))
	}

	diff := diffs[0]
	if diff.Path != "test.txt" {
		t.Errorf("Expected path 'test.txt', got '%s'", diff.Path)
	}
	if diff.Status != protos.DiffFileStatus_DIFF_FILE_STATUS_MODIFIED {
		t.Errorf("Expected status MODIFIED, got %v", diff.Status)
	}

	oldHash := base64.URLEncoding.EncodeToString(diff.OldContentHash)
	isBinary, err := isBinaryFileTest(oldHash)
	if err != nil {
		t.Fatalf("Failed to check if old file is binary: %v", err)
	}
	if isBinary {
		t.Error("Old UTF-16LE file should not be detected as binary")
	}

	newHash := base64.URLEncoding.EncodeToString(diff.NewContentHash)
	isBinary, err = isBinaryFileTest(newHash)
	if err != nil {
		t.Fatalf("Failed to check if new file is binary: %v", err)
	}
	if isBinary {
		t.Error("New UTF-16LE file should not be detected as binary")
	}
}

func TestResolveDiff_BinaryFileModified(t *testing.T) {
	env := setupTestEnvironment(t, "")
	defer env.cleanup()

	ctx := context.Background()

	repoPath, err := os.MkdirTemp("", "pogo-test-repo-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(repoPath)

	repoId, _, err := initializeRepository(ctx, repoPath, "test-binary-modified", env.serverAddr)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	c, err := client.OpenFromFile(ctx, repoPath)
	if err != nil {
		t.Fatalf("Failed to open client: %v", err)
	}
	defer c.Close()

	binaryContent1 := []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD, 0xFC}
	if err := os.WriteFile(filepath.Join(c.Location, "test.bin"), binaryContent1, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	if err := c.PushFull(false); err != nil {
		t.Fatalf("Failed to push: %v", err)
	}

	_, changeName1, err := c.NewChange(nil, nil)
	if err != nil {
		t.Fatalf("Failed to create new change: %v", err)
	}

	if err := c.Edit(changeName1); err != nil {
		t.Fatalf("Failed to edit change: %v", err)
	}

	binaryContent2 := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00, 0x11}
	if err := os.WriteFile(filepath.Join(c.Location, "test.bin"), binaryContent2, 0644); err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	if err := c.PushFull(false); err != nil {
		t.Fatalf("Failed to push: %v", err)
	}

	change1Id, err := db.Q.FindChangeByNameFuzzy(ctx, repoId, changeName1)
	if err != nil {
		t.Fatalf("Failed to find change: %v", err)
	}

	parents, err := db.Q.GetChangeParents(ctx, change1Id)
	if err != nil {
		t.Fatalf("Failed to get parents: %v", err)
	}
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	parent0Name := parents[0].Name

	diffs, err := server.ResolveDiff(ctx, repoId, &parent0Name, &changeName1, nil)
	if err != nil {
		t.Fatalf("ResolveDiff failed: %v", err)
	}

	if len(diffs) != 1 {
		t.Fatalf("Expected 1 diff, got %d", len(diffs))
	}

	diff := diffs[0]
	if diff.Path != "test.bin" {
		t.Errorf("Expected path 'test.bin', got '%s'", diff.Path)
	}
	if diff.Status != protos.DiffFileStatus_DIFF_FILE_STATUS_BINARY {
		t.Errorf("Expected status BINARY, got %v", diff.Status)
	}

	oldHash := base64.URLEncoding.EncodeToString(diff.OldContentHash)
	isBinary, err := isBinaryFileTest(oldHash)
	if err != nil {
		t.Fatalf("Failed to check if old file is binary: %v", err)
	}
	if !isBinary {
		t.Error("Old file should be detected as binary")
	}

	newHash := base64.URLEncoding.EncodeToString(diff.NewContentHash)
	isBinary, err = isBinaryFileTest(newHash)
	if err != nil {
		t.Fatalf("Failed to check if new file is binary: %v", err)
	}
	if !isBinary {
		t.Error("New file should be detected as binary")
	}
}

func TestResolveDiff_FileAdded(t *testing.T) {
	env := setupTestEnvironment(t, "")
	defer env.cleanup()

	ctx := context.Background()

	repoPath, err := os.MkdirTemp("", "pogo-test-repo-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(repoPath)

	repoId, _, err := initializeRepository(ctx, repoPath, "test-file-added", env.serverAddr)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	c, err := client.OpenFromFile(ctx, repoPath)
	if err != nil {
		t.Fatalf("Failed to open client: %v", err)
	}
	defer c.Close()

	if err := os.WriteFile(filepath.Join(c.Location, "existing.txt"), []byte("Existing\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	if err := c.PushFull(false); err != nil {
		t.Fatalf("Failed to push: %v", err)
	}

	_, changeName1, err := c.NewChange(nil, nil)
	if err != nil {
		t.Fatalf("Failed to create new change: %v", err)
	}

	if err := c.Edit(changeName1); err != nil {
		t.Fatalf("Failed to edit change: %v", err)
	}

	if err := os.WriteFile(filepath.Join(c.Location, "new.txt"), []byte("New file\n"), 0644); err != nil {
		t.Fatalf("Failed to write new file: %v", err)
	}

	if err := c.PushFull(false); err != nil {
		t.Fatalf("Failed to push: %v", err)
	}

	change1Id, err := db.Q.FindChangeByNameFuzzy(ctx, repoId, changeName1)
	if err != nil {
		t.Fatalf("Failed to find change: %v", err)
	}

	parents, err := db.Q.GetChangeParents(ctx, change1Id)
	if err != nil {
		t.Fatalf("Failed to get parents: %v", err)
	}
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	parent0Name := parents[0].Name

	diffs, err := server.ResolveDiff(ctx, repoId, &parent0Name, &changeName1, nil)
	if err != nil {
		t.Fatalf("ResolveDiff failed: %v", err)
	}

	if len(diffs) != 1 {
		t.Fatalf("Expected 1 diff, got %d", len(diffs))
	}

	diff := diffs[0]
	if diff.Path != "new.txt" {
		t.Errorf("Expected path 'new.txt', got '%s'", diff.Path)
	}
	if diff.Status != protos.DiffFileStatus_DIFF_FILE_STATUS_ADDED {
		t.Errorf("Expected status ADDED, got %v", diff.Status)
	}
	if diff.OldContentHash != nil {
		t.Error("Expected OldContentHash to be nil for added file")
	}
	if diff.NewContentHash == nil {
		t.Error("Expected NewContentHash to be non-nil for added file")
	}
}

func TestResolveDiff_FileDeleted(t *testing.T) {
	env := setupTestEnvironment(t, "")
	defer env.cleanup()

	ctx := context.Background()

	repoPath, err := os.MkdirTemp("", "pogo-test-repo-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(repoPath)

	repoId, _, err := initializeRepository(ctx, repoPath, "test-file-deleted", env.serverAddr)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	c, err := client.OpenFromFile(ctx, repoPath)
	if err != nil {
		t.Fatalf("Failed to open client: %v", err)
	}
	defer c.Close()

	if err := os.WriteFile(filepath.Join(c.Location, "to-delete.txt"), []byte("Delete me\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Location, "keep.txt"), []byte("Keep me\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	if err := c.PushFull(false); err != nil {
		t.Fatalf("Failed to push: %v", err)
	}

	_, changeName1, err := c.NewChange(nil, nil)
	if err != nil {
		t.Fatalf("Failed to create new change: %v", err)
	}

	if err := c.Edit(changeName1); err != nil {
		t.Fatalf("Failed to edit change: %v", err)
	}

	if err := os.Remove(filepath.Join(c.Location, "to-delete.txt")); err != nil {
		t.Fatalf("Failed to delete file: %v", err)
	}

	if err := c.PushFull(false); err != nil {
		t.Fatalf("Failed to push: %v", err)
	}

	change1Id, err := db.Q.FindChangeByNameFuzzy(ctx, repoId, changeName1)
	if err != nil {
		t.Fatalf("Failed to find change: %v", err)
	}

	parents, err := db.Q.GetChangeParents(ctx, change1Id)
	if err != nil {
		t.Fatalf("Failed to get parents: %v", err)
	}
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	parent0Name := parents[0].Name

	diffs, err := server.ResolveDiff(ctx, repoId, &parent0Name, &changeName1, nil)
	if err != nil {
		t.Fatalf("ResolveDiff failed: %v", err)
	}

	if len(diffs) != 1 {
		t.Fatalf("Expected 1 diff, got %d", len(diffs))
	}

	diff := diffs[0]
	if diff.Path != "to-delete.txt" {
		t.Errorf("Expected path 'to-delete.txt', got '%s'", diff.Path)
	}
	if diff.Status != protos.DiffFileStatus_DIFF_FILE_STATUS_DELETED {
		t.Errorf("Expected status DELETED, got %v", diff.Status)
	}
	if diff.OldContentHash == nil {
		t.Error("Expected OldContentHash to be non-nil for deleted file")
	}
	if diff.NewContentHash != nil {
		t.Error("Expected NewContentHash to be nil for deleted file")
	}
}

func TestResolveDiff_TextToBinaryChange(t *testing.T) {
	env := setupTestEnvironment(t, "")
	defer env.cleanup()

	ctx := context.Background()

	repoPath, err := os.MkdirTemp("", "pogo-test-repo-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(repoPath)

	repoId, _, err := initializeRepository(ctx, repoPath, "test-text-to-binary", env.serverAddr)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	c, err := client.OpenFromFile(ctx, repoPath)
	if err != nil {
		t.Fatalf("Failed to open client: %v", err)
	}
	defer c.Close()

	if err := os.WriteFile(filepath.Join(c.Location, "test.dat"), []byte("Hello, World!\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	if err := c.PushFull(false); err != nil {
		t.Fatalf("Failed to push: %v", err)
	}

	_, changeName1, err := c.NewChange(nil, nil)
	if err != nil {
		t.Fatalf("Failed to create new change: %v", err)
	}

	if err := c.Edit(changeName1); err != nil {
		t.Fatalf("Failed to edit change: %v", err)
	}

	binaryContent := []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD, 0xFC}
	if err := os.WriteFile(filepath.Join(c.Location, "test.dat"), binaryContent, 0644); err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	if err := c.PushFull(false); err != nil {
		t.Fatalf("Failed to push: %v", err)
	}

	change1Id, err := db.Q.FindChangeByNameFuzzy(ctx, repoId, changeName1)
	if err != nil {
		t.Fatalf("Failed to find change: %v", err)
	}

	parents, err := db.Q.GetChangeParents(ctx, change1Id)
	if err != nil {
		t.Fatalf("Failed to get parents: %v", err)
	}
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	parent0Name := parents[0].Name

	diffs, err := server.ResolveDiff(ctx, repoId, &parent0Name, &changeName1, nil)
	if err != nil {
		t.Fatalf("ResolveDiff failed: %v", err)
	}

	if len(diffs) != 1 {
		t.Fatalf("Expected 1 diff, got %d", len(diffs))
	}

	diff := diffs[0]
	if diff.Path != "test.dat" {
		t.Errorf("Expected path 'test.dat', got '%s'", diff.Path)
	}
	if diff.Status != protos.DiffFileStatus_DIFF_FILE_STATUS_BINARY {
		t.Errorf("Expected status BINARY (because new version is binary), got %v", diff.Status)
	}
}

func TestResolveDiff_MultipleFilesChanged(t *testing.T) {
	env := setupTestEnvironment(t, "")
	defer env.cleanup()

	ctx := context.Background()

	repoPath, err := os.MkdirTemp("", "pogo-test-repo-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(repoPath)

	repoId, _, err := initializeRepository(ctx, repoPath, "test-multiple-files", env.serverAddr)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	c, err := client.OpenFromFile(ctx, repoPath)
	if err != nil {
		t.Fatalf("Failed to open client: %v", err)
	}
	defer c.Close()

	if err := os.WriteFile(filepath.Join(c.Location, "file1.txt"), []byte("File 1\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Location, "file2.txt"), []byte("File 2\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Location, "file3.txt"), []byte("File 3\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	if err := c.PushFull(false); err != nil {
		t.Fatalf("Failed to push: %v", err)
	}

	_, changeName1, err := c.NewChange(nil, nil)
	if err != nil {
		t.Fatalf("Failed to create new change: %v", err)
	}

	if err := c.Edit(changeName1); err != nil {
		t.Fatalf("Failed to edit change: %v", err)
	}

	if err := os.WriteFile(filepath.Join(c.Location, "file1.txt"), []byte("Modified File 1\n"), 0644); err != nil {
		t.Fatalf("Failed to modify file1: %v", err)
	}
	if err := os.Remove(filepath.Join(c.Location, "file2.txt")); err != nil {
		t.Fatalf("Failed to delete file2: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Location, "file4.txt"), []byte("New File 4\n"), 0644); err != nil {
		t.Fatalf("Failed to add file4: %v", err)
	}

	if err := c.PushFull(false); err != nil {
		t.Fatalf("Failed to push: %v", err)
	}

	change1Id, err := db.Q.FindChangeByNameFuzzy(ctx, repoId, changeName1)
	if err != nil {
		t.Fatalf("Failed to find change: %v", err)
	}

	parents, err := db.Q.GetChangeParents(ctx, change1Id)
	if err != nil {
		t.Fatalf("Failed to get parents: %v", err)
	}
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	parent0Name := parents[0].Name

	diffs, err := server.ResolveDiff(ctx, repoId, &parent0Name, &changeName1, nil)
	if err != nil {
		t.Fatalf("ResolveDiff failed: %v", err)
	}

	if len(diffs) != 3 {
		t.Fatalf("Expected 3 diffs, got %d", len(diffs))
	}

	statusByPath := make(map[string]protos.DiffFileStatus)
	for _, diff := range diffs {
		statusByPath[diff.Path] = diff.Status
	}

	if statusByPath["file1.txt"] != protos.DiffFileStatus_DIFF_FILE_STATUS_MODIFIED {
		t.Errorf("Expected file1.txt to be MODIFIED, got %v", statusByPath["file1.txt"])
	}
	if statusByPath["file2.txt"] != protos.DiffFileStatus_DIFF_FILE_STATUS_DELETED {
		t.Errorf("Expected file2.txt to be DELETED, got %v", statusByPath["file2.txt"])
	}
	if statusByPath["file4.txt"] != protos.DiffFileStatus_DIFF_FILE_STATUS_ADDED {
		t.Errorf("Expected file4.txt to be ADDED, got %v", statusByPath["file4.txt"])
	}
}

func isBinaryFileTest(hash string) (bool, error) {
	f, fileType, err := filecontents.OpenFileByHashWithType(hash)
	if err != nil {
		return false, fmt.Errorf("open file by hash: %w", err)
	}
	defer f.Close()

	return fileType.Binary, nil
}

func TestGenerateUnifiedDiff_ShortHash(t *testing.T) {
	oldContent := "Hello World\n"
	newContent := "Hello Pogo\n"

	oldHash := "abc123"
	newHash := "def456"

	_, err := server.GenerateUnifiedDiff(oldContent, newContent, oldHash, newHash, "test.txt")
	if err != nil {
		t.Fatalf("GenerateUnifiedDiff with short hash failed: %v", err)
	}
}

func TestGenerateUnifiedDiff_EmptyHash(t *testing.T) {
	oldContent := "Hello World\n"
	newContent := "Hello Pogo\n"

	oldHash := ""
	newHash := ""

	_, err := server.GenerateUnifiedDiff(oldContent, newContent, oldHash, newHash, "test.txt")
	if err != nil {
		t.Fatalf("GenerateUnifiedDiff with empty hash failed: %v", err)
	}
}

func TestGenerateDiffBlocks_ContentWithoutTrailingNewline(t *testing.T) {
	oldContent := "line1\nline2\nline3"
	newContent := "line1\nline2 modified\nline3"

	blocks, err := server.GenerateDiffBlocks(oldContent, newContent)
	if err != nil {
		t.Fatalf("GenerateDiffBlocks failed: %v", err)
	}

	if len(blocks) == 0 {
		t.Fatal("Expected at least one block")
	}
}

func TestGenerateDiffBlocks_LargeFileWithManyChanges(t *testing.T) {
	var oldLines, newLines []string
	for i := 1; i <= 100; i++ {
		if i%10 == 5 {
			oldLines = append(oldLines, fmt.Sprintf("line %d old", i))
			newLines = append(newLines, fmt.Sprintf("line %d new", i))
		} else {
			line := fmt.Sprintf("line %d", i)
			oldLines = append(oldLines, line)
			newLines = append(newLines, line)
		}
	}

	oldContent := ""
	for i, line := range oldLines {
		oldContent += line
		if i < len(oldLines)-1 {
			oldContent += "\n"
		}
	}

	newContent := ""
	for i, line := range newLines {
		newContent += line
		if i < len(newLines)-1 {
			newContent += "\n"
		}
	}

	blocks, err := server.GenerateDiffBlocks(oldContent, newContent)
	if err != nil {
		t.Fatalf("GenerateDiffBlocks failed: %v", err)
	}

	if len(blocks) == 0 {
		t.Fatal("Expected at least one block")
	}
}

func TestGenerateDiffBlocks_MultipleHunksNearEndOfFile(t *testing.T) {
	var oldLines []string
	for i := 1; i <= 75; i++ {
		oldLines = append(oldLines, fmt.Sprintf("line %d", i))
	}

	var newLines []string
	for i := 1; i <= 75; i++ {
		if i == 73 {
			newLines = append(newLines, "line 73 modified")
		} else {
			newLines = append(newLines, fmt.Sprintf("line %d", i))
		}
	}

	oldContent := ""
	for i, line := range oldLines {
		oldContent += line
		if i < len(oldLines)-1 {
			oldContent += "\n"
		}
	}

	newContent := ""
	for i, line := range newLines {
		newContent += line
		if i < len(newLines)-1 {
			newContent += "\n"
		}
	}

	blocks, err := server.GenerateDiffBlocks(oldContent, newContent)
	if err != nil {
		t.Fatalf("GenerateDiffBlocks failed with 75 line file: %v", err)
	}

	if len(blocks) == 0 {
		t.Fatal("Expected at least one block")
	}
}
