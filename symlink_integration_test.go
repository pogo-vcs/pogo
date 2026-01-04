package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	
	"github.com/pogo-vcs/pogo/client"
)

// TestSymlinkPushPull tests pushing and pulling symlinks
func TestSymlinkPushPull(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Start test server
	ctx, cancel := setupTestServer(t)
	defer cancel()

	// Create first client location
	client1Dir := t.TempDir()
	
	// Create a regular file
	targetFile := filepath.Join(client1Dir, "target.txt")
	if err := os.WriteFile(targetFile, []byte("target content"), 0644); err != nil {
		t.Fatal(err)
	}
	
	// Create a symlink pointing to the target file
	symlinkPath := filepath.Join(client1Dir, "link.txt")
	if err := os.Symlink("target.txt", symlinkPath); err != nil {
		t.Skip("Symlink creation not supported:", err)
	}
	
	// Initialize and push from client1
	repoName := "test-symlink-repo"
	client1 := initTestClient(t, ctx, client1Dir, repoName)
	if err := client1.PushFull(false); err != nil {
		t.Fatalf("Push failed: %v", err)
	}
	
	// Create second client location and clone
	client2Dir := t.TempDir()
	client2 := cloneTestRepo(t, ctx, client2Dir, repoName)
	if err := client2.Edit("main"); err != nil {
		t.Fatalf("Edit failed: %v", err)
	}
	
	// Verify symlink was created in client2
	client2Symlink := filepath.Join(client2Dir, "link.txt")
	info, err := os.Lstat(client2Symlink)
	if err != nil {
		t.Fatalf("Symlink not found in client2: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("Expected symlink, got regular file")
	}
	
	// Verify symlink target
	target, err := os.Readlink(client2Symlink)
	if err != nil {
		t.Fatalf("Failed to read symlink: %v", err)
	}
	if target != "target.txt" {
		t.Errorf("Symlink target = %q, want %q", target, "target.txt")
	}
	
	// Verify target file exists
	client2Target := filepath.Join(client2Dir, "target.txt")
	content, err := os.ReadFile(client2Target)
	if err != nil {
		t.Fatalf("Target file not found: %v", err)
	}
	if string(content) != "target content" {
		t.Errorf("Target content = %q, want %q", string(content), "target content")
	}
}

// TestSymlinkToDirectory tests symlinks pointing to directories
func TestSymlinkToDirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := setupTestServer(t)
	defer cancel()

	client1Dir := t.TempDir()
	
	// Create a directory with a file
	targetDir := filepath.Join(client1Dir, "dir")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "file.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	
	// Create symlink to directory
	symlinkPath := filepath.Join(client1Dir, "linkdir")
	if err := os.Symlink("dir", symlinkPath); err != nil {
		t.Skip("Symlink creation not supported:", err)
	}
	
	repoName := "test-symlink-dir-repo"
	client1 := initTestClient(t, ctx, client1Dir, repoName)
	if err := client1.PushFull(false); err != nil {
		t.Fatalf("Push failed: %v", err)
	}
	
	// Clone to client2
	client2Dir := t.TempDir()
	client2 := cloneTestRepo(t, ctx, client2Dir, repoName)
	if err := client2.Edit("main"); err != nil {
		t.Fatalf("Edit failed: %v", err)
	}
	
	// Verify directory symlink
	client2Symlink := filepath.Join(client2Dir, "linkdir")
	info, err := os.Lstat(client2Symlink)
	if err != nil {
		t.Fatalf("Symlink not found: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("Expected symlink to directory")
	}
	
	target, err := os.Readlink(client2Symlink)
	if err != nil {
		t.Fatalf("Failed to read symlink: %v", err)
	}
	if target != "dir" {
		t.Errorf("Symlink target = %q, want %q", target, "dir")
	}
}

// TestSymlinkOutsideRepo tests that symlinks pointing outside repo are rejected
func TestSymlinkOutsideRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := setupTestServer(t)
	defer cancel()

	client1Dir := t.TempDir()
	
	// Create symlink pointing outside repo
	symlinkPath := filepath.Join(client1Dir, "bad-link")
	if err := os.Symlink("../outside.txt", symlinkPath); err != nil {
		t.Skip("Symlink creation not supported:", err)
	}
	
	repoName := "test-bad-symlink-repo"
	client1 := initTestClient(t, ctx, client1Dir, repoName)
	
	// Push should fail
	err := client1.PushFull(false)
	if err == nil {
		t.Error("Expected push to fail for symlink pointing outside repo")
	}
}

// TestSymlinkChain tests chained symlinks (A -> B -> C)
func TestSymlinkChain(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := setupTestServer(t)
	defer cancel()

	client1Dir := t.TempDir()
	
	// Create target file
	if err := os.WriteFile(filepath.Join(client1Dir, "target.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	
	// Create chain: link1 -> link2 -> target.txt
	if err := os.Symlink("target.txt", filepath.Join(client1Dir, "link2")); err != nil {
		t.Skip("Symlink creation not supported:", err)
	}
	if err := os.Symlink("link2", filepath.Join(client1Dir, "link1")); err != nil {
		t.Skip("Symlink creation not supported:", err)
	}
	
	repoName := "test-symlink-chain-repo"
	client1 := initTestClient(t, ctx, client1Dir, repoName)
	if err := client1.PushFull(false); err != nil {
		t.Fatalf("Push failed: %v", err)
	}
	
	// Clone and verify
	client2Dir := t.TempDir()
	client2 := cloneTestRepo(t, ctx, client2Dir, repoName)
	if err := client2.Edit("main"); err != nil {
		t.Fatalf("Edit failed: %v", err)
	}
	
	// Verify all links in chain
	link1 := filepath.Join(client2Dir, "link1")
	target1, err := os.Readlink(link1)
	if err != nil {
		t.Fatalf("Failed to read link1: %v", err)
	}
	if target1 != "link2" {
		t.Errorf("link1 target = %q, want %q", target1, "link2")
	}
	
	link2 := filepath.Join(client2Dir, "link2")
	target2, err := os.Readlink(link2)
	if err != nil {
		t.Fatalf("Failed to read link2: %v", err)
	}
	if target2 != "target.txt" {
		t.Errorf("link2 target = %q, want %q", target2, "target.txt")
	}
}

// TestSymlinkModification tests changing symlink targets
func TestSymlinkModification(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := setupTestServer(t)
	defer cancel()

	client1Dir := t.TempDir()
	
	// Create two target files
	if err := os.WriteFile(filepath.Join(client1Dir, "target1.txt"), []byte("content1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(client1Dir, "target2.txt"), []byte("content2"), 0644); err != nil {
		t.Fatal(err)
	}
	
	// Create symlink to target1
	symlinkPath := filepath.Join(client1Dir, "link.txt")
	if err := os.Symlink("target1.txt", symlinkPath); err != nil {
		t.Skip("Symlink creation not supported:", err)
	}
	
	repoName := "test-symlink-modify-repo"
	client1 := initTestClient(t, ctx, client1Dir, repoName)
	if err := client1.PushFull(false); err != nil {
		t.Fatalf("Initial push failed: %v", err)
	}
	
	// Modify symlink to point to target2
	if err := os.Remove(symlinkPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target2.txt", symlinkPath); err != nil {
		t.Fatal(err)
	}
	
	// Push again
	if _, _, err := client1.NewChange(nil, nil); err != nil {
		t.Fatalf("NewChange failed: %v", err)
	}
	if err := client1.PushFull(false); err != nil {
		t.Fatalf("Second push failed: %v", err)
	}
	
	// Clone and verify new target
	client2Dir := t.TempDir()
	client2 := cloneTestRepo(t, ctx, client2Dir, repoName)
	if err := client2.Edit("main"); err != nil {
		t.Fatalf("Edit failed: %v", err)
	}
	
	client2Symlink := filepath.Join(client2Dir, "link.txt")
	target, err := os.Readlink(client2Symlink)
	if err != nil {
		t.Fatalf("Failed to read symlink: %v", err)
	}
	if target != "target2.txt" {
		t.Errorf("Symlink target = %q, want %q", target, "target2.txt")
	}
}

// Helper functions (these would need to be implemented based on your test setup)
func setupTestServer(t *testing.T) (context.Context, context.CancelFunc) {
	// Implementation depends on your test infrastructure
	// This is a placeholder that skips tests when called
	t.Skip("Test server setup not implemented - placeholder for future integration tests")
	return context.Background(), func() {}
}

func initTestClient(t *testing.T, ctx context.Context, dir, repoName string) *client.Client {
	// Implementation depends on your test infrastructure
	// This is a placeholder
	t.Skip("Client initialization not implemented - placeholder for future integration tests")
	return nil
}

func cloneTestRepo(t *testing.T, ctx context.Context, dir, repoName string) *client.Client {
	// Implementation depends on your test infrastructure
	// This is a placeholder
	t.Skip("Clone not implemented - placeholder for future integration tests")
	return nil
}
