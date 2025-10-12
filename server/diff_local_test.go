//go:build fakekeyring

package server_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/protos"
)

func TestDiffLocal_NoChanges(t *testing.T) {
	env := setupTestEnvironment(t, "")
	defer env.cleanup()

	ctx := context.Background()

	repoPath, err := os.MkdirTemp("", "pogo-test-repo-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(repoPath)

	_, _, err = initializeRepository(ctx, repoPath, "test-diff-local-no-changes", env.serverAddr)
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

	diffs, err := c.DiffLocal()
	if err != nil {
		t.Fatalf("DiffLocal failed: %v", err)
	}

	if len(diffs) != 0 {
		t.Errorf("Expected no diffs, got %d", len(diffs))
	}
}

func TestDiffLocal_FileModified(t *testing.T) {
	env := setupTestEnvironment(t, "")
	defer env.cleanup()

	ctx := context.Background()

	repoPath, err := os.MkdirTemp("", "pogo-test-repo-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(repoPath)

	_, _, err = initializeRepository(ctx, repoPath, "test-diff-local-modified", env.serverAddr)
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

	if err := os.WriteFile(filepath.Join(c.Location, "test.txt"), []byte("Hello, Pogo!\n"), 0644); err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	diffs, err := c.DiffLocal()
	if err != nil {
		t.Fatalf("DiffLocal failed: %v", err)
	}

	if len(diffs) != 1 {
		t.Fatalf("Expected 1 diff, got %d", len(diffs))
	}

	diff := diffs[0]
	if diff.Path != "test.txt" {
		t.Errorf("Expected path 'test.txt', got '%s'", diff.Path)
	}
	if diff.Status != protos.DiffFileStatus_DIFF_FILE_STATUS_MODIFIED {
		t.Errorf("Expected MODIFIED status, got %v", diff.Status)
	}
}

func TestDiffLocal_FileAdded(t *testing.T) {
	env := setupTestEnvironment(t, "")
	defer env.cleanup()

	ctx := context.Background()

	repoPath, err := os.MkdirTemp("", "pogo-test-repo-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(repoPath)

	_, _, err = initializeRepository(ctx, repoPath, "test-diff-local-added", env.serverAddr)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	c, err := client.OpenFromFile(ctx, repoPath)
	if err != nil {
		t.Fatalf("Failed to open client: %v", err)
	}
	defer c.Close()

	if err := os.WriteFile(filepath.Join(c.Location, "existing.txt"), []byte("Existing file\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	if err := c.PushFull(false); err != nil {
		t.Fatalf("Failed to push: %v", err)
	}

	if err := os.WriteFile(filepath.Join(c.Location, "new.txt"), []byte("New file\n"), 0644); err != nil {
		t.Fatalf("Failed to write new file: %v", err)
	}

	diffs, err := c.DiffLocal()
	if err != nil {
		t.Fatalf("DiffLocal failed: %v", err)
	}

	if len(diffs) != 1 {
		t.Fatalf("Expected 1 diff, got %d", len(diffs))
	}

	diff := diffs[0]
	if diff.Path != "new.txt" {
		t.Errorf("Expected path 'new.txt', got '%s'", diff.Path)
	}
	if diff.Status != protos.DiffFileStatus_DIFF_FILE_STATUS_ADDED {
		t.Errorf("Expected ADDED status, got %v", diff.Status)
	}
}

func TestDiffLocal_FileDeleted(t *testing.T) {
	env := setupTestEnvironment(t, "")
	defer env.cleanup()

	ctx := context.Background()

	repoPath, err := os.MkdirTemp("", "pogo-test-repo-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(repoPath)

	_, _, err = initializeRepository(ctx, repoPath, "test-diff-local-deleted", env.serverAddr)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	c, err := client.OpenFromFile(ctx, repoPath)
	if err != nil {
		t.Fatalf("Failed to open client: %v", err)
	}
	defer c.Close()

	if err := os.WriteFile(filepath.Join(c.Location, "to-delete.txt"), []byte("To be deleted\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	if err := c.PushFull(false); err != nil {
		t.Fatalf("Failed to push: %v", err)
	}

	if err := os.Remove(filepath.Join(c.Location, "to-delete.txt")); err != nil {
		t.Fatalf("Failed to remove file: %v", err)
	}

	diffs, err := c.DiffLocal()
	if err != nil {
		t.Fatalf("DiffLocal failed: %v", err)
	}

	if len(diffs) != 1 {
		t.Fatalf("Expected 1 diff, got %d", len(diffs))
	}

	diff := diffs[0]
	if diff.Path != "to-delete.txt" {
		t.Errorf("Expected path 'to-delete.txt', got '%s'", diff.Path)
	}
	if diff.Status != protos.DiffFileStatus_DIFF_FILE_STATUS_DELETED {
		t.Errorf("Expected DELETED status, got %v", diff.Status)
	}
}

func TestDiffLocal_MultipleFilesChanged(t *testing.T) {
	env := setupTestEnvironment(t, "")
	defer env.cleanup()

	ctx := context.Background()

	repoPath, err := os.MkdirTemp("", "pogo-test-repo-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(repoPath)

	_, _, err = initializeRepository(ctx, repoPath, "test-diff-local-multiple", env.serverAddr)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	c, err := client.OpenFromFile(ctx, repoPath)
	if err != nil {
		t.Fatalf("Failed to open client: %v", err)
	}
	defer c.Close()

	if err := os.WriteFile(filepath.Join(c.Location, "unchanged.txt"), []byte("Unchanged\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Location, "to-modify.txt"), []byte("Original\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Location, "to-delete.txt"), []byte("To delete\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	if err := c.PushFull(false); err != nil {
		t.Fatalf("Failed to push: %v", err)
	}

	if err := os.WriteFile(filepath.Join(c.Location, "to-modify.txt"), []byte("Modified\n"), 0644); err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}
	if err := os.Remove(filepath.Join(c.Location, "to-delete.txt")); err != nil {
		t.Fatalf("Failed to remove file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Location, "new-file.txt"), []byte("New file\n"), 0644); err != nil {
		t.Fatalf("Failed to write new file: %v", err)
	}

	diffs, err := c.DiffLocal()
	if err != nil {
		t.Fatalf("DiffLocal failed: %v", err)
	}

	if len(diffs) != 3 {
		t.Fatalf("Expected 3 diffs, got %d", len(diffs))
	}

	diffsByPath := make(map[string]protos.DiffFileStatus)
	for _, diff := range diffs {
		diffsByPath[diff.Path] = diff.Status
	}

	if status, ok := diffsByPath["to-modify.txt"]; !ok || status != protos.DiffFileStatus_DIFF_FILE_STATUS_MODIFIED {
		t.Errorf("Expected to-modify.txt to be MODIFIED")
	}
	if status, ok := diffsByPath["to-delete.txt"]; !ok || status != protos.DiffFileStatus_DIFF_FILE_STATUS_DELETED {
		t.Errorf("Expected to-delete.txt to be DELETED")
	}
	if status, ok := diffsByPath["new-file.txt"]; !ok || status != protos.DiffFileStatus_DIFF_FILE_STATUS_ADDED {
		t.Errorf("Expected new-file.txt to be ADDED")
	}
}
