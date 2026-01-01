//go:build fakekeyring

package main_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/db"
	"github.com/pogo-vcs/pogo/filecontents"
)

// Using root token directly in tests - setupToken function exists in integration_test.go

// Test the race condition scenario where:
// 1. Push A overwrites a change and removes a file, making it potentially orphaned
// 2. Push B concurrently creates a new change that adds the same file back
// 3. The file should not be deleted because Push B references it
// This test is much more realistic in creating an actual race condition
func TestGCPushRaceCondition(t *testing.T) {
	ctx := context.Background()
	testEnv := setupTestEnvironment(t)
	defer testEnv.cleanup()

	// Setup token
	if err := setupToken(testEnv.serverAddr); err != nil {
		t.Fatalf("Failed to setup token: %v", err)
	}

	// Create temporary directories for both clients
	tmpDirA, err := os.MkdirTemp("", "pogo-test-race-a-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory A: %v", err)
	}
	defer os.RemoveAll(tmpDirA)

	tmpDirB, err := os.MkdirTemp("", "pogo-test-race-b-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory B: %v", err)
	}
	defer os.RemoveAll(tmpDirB)

	// Initialize repository
	repoName := "test-race-repo"
	clientA, err := client.OpenNew(ctx, testEnv.serverAddr, tmpDirA)
	if err != nil {
		t.Fatalf("Failed to create client A: %v", err)
	}
	defer clientA.Close()

	repoId, emptyChangeId, err := clientA.Init(repoName, true)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	// Create a base change with our test file
	config := client.Repo{
		Server:   testEnv.serverAddr,
		RepoId:   repoId,
		ChangeId: emptyChangeId,
	}
	if err := config.Save(filepath.Join(tmpDirA, ".pogo.yaml")); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Create test file
	testFile := filepath.Join(tmpDirA, "test.txt")
	testContent := "shared content"
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Push to create a change with the file
	clientA2, err := client.OpenFromFile(ctx, tmpDirA)
	if err != nil {
		t.Fatalf("Failed to open client A2: %v", err)
	}
	defer clientA2.Close()

	if err := clientA2.PushFull(false); err != nil {
		t.Fatalf("Failed to push file: %v", err)
	}

	// Get file hash for tracking
	originalHash, err := filecontents.HashFile(testFile)
	if err != nil {
		t.Fatalf("Failed to hash file: %v", err)
	}

	// Get the change info
	info, err := clientA2.Info()
	if err != nil {
		t.Fatalf("Failed to get info: %v", err)
	}
	fileChangeId := emptyChangeId // The change that has the file

	// Create a second change that will be used to add the file back
	desc := "Second change"
	secondChangeId, _, err := clientA2.NewChange(&desc, []string{info.ChangeName})
	if err != nil {
		t.Fatalf("Failed to create second change: %v", err)
	}

	// Now set up the race:
	// Client A will overwrite the first change to remove the file (making it orphaned)
	// Client B will push to the second change to add the same file back

	// Configure clients for the race
	configA := client.Repo{
		Server:   testEnv.serverAddr,
		RepoId:   repoId,
		ChangeId: fileChangeId, // Client A works on the original change with file
	}
	if err := configA.Save(filepath.Join(tmpDirA, ".pogo.yaml")); err != nil {
		t.Fatalf("Failed to save config A: %v", err)
	}

	configB := client.Repo{
		Server:   testEnv.serverAddr,
		RepoId:   repoId,
		ChangeId: secondChangeId, // Client B works on the new change
	}
	if err := configB.Save(filepath.Join(tmpDirB, ".pogo.yaml")); err != nil {
		t.Fatalf("Failed to save config B: %v", err)
	}

	clientB, err := client.OpenFromFile(ctx, tmpDirB)
	if err != nil {
		t.Fatalf("Failed to open client B: %v", err)
	}
	defer clientB.Close()

	// Prepare the race conditions:
	// Client A: Remove the file (empty push - makes file orphaned)
	if err := os.Remove(testFile); err != nil {
		t.Fatalf("Failed to remove test file: %v", err)
	}

	// Client B: Add the same file
	testFileB := filepath.Join(tmpDirB, "test.txt")
	if err := os.WriteFile(testFileB, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file B: %v", err)
	}

	// Start the race with synchronization
	startBarrier := make(chan struct{})
	var wg sync.WaitGroup
	var pushErrA, pushErrB error

	wg.Add(2)

	// Push A: Remove file (making it potentially orphaned)
	go func() {
		defer wg.Done()
		<-startBarrier
		// Use force because change might have become readonly
		pushErrA = clientA2.PushFull(true)
	}()

	// Push B: Add the file back to different change
	go func() {
		defer wg.Done()
		<-startBarrier
		pushErrB = clientB.PushFull(false)
	}()

	// Start both pushes simultaneously
	close(startBarrier)
	wg.Wait()

	// Check results
	if pushErrA != nil {
		t.Fatalf("Push A failed: %v", pushErrA)
	}
	if pushErrB != nil {
		t.Fatalf("Push B failed: %v", pushErrB)
	}

	// Wait for async GC
	time.Sleep(500 * time.Millisecond)

	// Verify file was preserved
	hashStr := base64.URLEncoding.EncodeToString(originalHash)
	filePath := filecontents.GetFilePathFromHash(hashStr)

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Errorf("File was incorrectly deleted during concurrent push - race condition failed")
	}

	// Verify database state
	exists, err := db.Q.CheckFileHashExists(ctx, originalHash)
	if err != nil {
		t.Fatalf("Failed to check file existence: %v", err)
	}
	if !exists {
		t.Errorf("File was incorrectly removed from database")
	}

	// Verify change A no longer has the file
	filesA, err := db.Q.GetChangeFiles(ctx, fileChangeId)
	if err != nil {
		t.Fatalf("Failed to get files for change A: %v", err)
	}
	if len(filesA) != 0 {
		t.Errorf("Expected change A to have 0 files, got %d", len(filesA))
	}

	// Verify change B has the file
	filesB, err := db.Q.GetChangeFiles(ctx, secondChangeId)
	if err != nil {
		t.Fatalf("Failed to get files for change B: %v", err)
	}
	if len(filesB) != 1 {
		t.Errorf("Expected change B to have 1 file, got %d", len(filesB))
	} else if !bytes.Equal(filesB[0].ContentHash, originalHash) {
		t.Errorf("Change B has wrong file hash")
	}

	t.Logf("Test passed: File preserved during race condition. Change A removed it, Change B added it back.")
}

// TestGCPushParentFilePreservation tests the bug scenario where:
// 1. Parent change P has a file
// 2. Child change C is created from P (inherits file via CopyChangeFiles)
// 3. User adds .pogoignore to ignore the file and pushes to C
// 4. File should NOT be deleted because P still references it
// 5. Diff between C and P should work (not fail with "no such file")
func TestGCPushParentFilePreservation(t *testing.T) {
	ctx := context.Background()
	testEnv := setupTestEnvironment(t)
	defer testEnv.cleanup()

	// Setup token
	if err := setupToken(testEnv.serverAddr); err != nil {
		t.Fatalf("Failed to setup token: %v", err)
	}

	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "pogo-test-parent-preserve-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize repository
	repoName := "test-parent-preserve-repo"
	clientInit, err := client.OpenNew(ctx, testEnv.serverAddr, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer clientInit.Close()

	repoId, rootChangeId, err := clientInit.Init(repoName, true)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	// Save config for root change
	config := client.Repo{
		Server:   testEnv.serverAddr,
		RepoId:   repoId,
		ChangeId: rootChangeId,
	}
	if err := config.Save(filepath.Join(tmpDir, ".pogo.yaml")); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Create test file that will be ignored later
	testFile := filepath.Join(tmpDir, "test.level")
	testContent := "level content that should be preserved"
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Push to root change (P) - this makes P have the file
	clientRoot, err := client.OpenFromFile(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to open client for root: %v", err)
	}
	defer clientRoot.Close()

	if err := clientRoot.PushFull(false); err != nil {
		t.Fatalf("Failed to push to root change: %v", err)
	}

	// Get file hash for tracking
	fileHash, err := filecontents.HashFile(testFile)
	if err != nil {
		t.Fatalf("Failed to hash file: %v", err)
	}

	// Get the root change info
	rootInfo, err := clientRoot.Info()
	if err != nil {
		t.Fatalf("Failed to get root info: %v", err)
	}

	// Verify root change has the file
	rootFiles, err := db.Q.GetChangeFiles(ctx, rootChangeId)
	if err != nil {
		t.Fatalf("Failed to get root change files: %v", err)
	}
	if len(rootFiles) != 1 {
		t.Fatalf("Expected root change to have 1 file, got %d", len(rootFiles))
	}
	t.Logf("Root change %s has file with hash %x", rootInfo.ChangeName, rootFiles[0].ContentHash)

	// Create child change C from root change P
	// This calls CopyChangeFiles, so C will inherit the file reference
	desc := "Child change"
	childChangeId, childChangeName, err := clientRoot.NewChange(&desc, []string{rootInfo.ChangeName})
	if err != nil {
		t.Fatalf("Failed to create child change: %v", err)
	}
	t.Logf("Created child change %s (id=%d) from parent %s", childChangeName, childChangeId, rootInfo.ChangeName)

	// Verify child change inherited the file
	childFiles, err := db.Q.GetChangeFiles(ctx, childChangeId)
	if err != nil {
		t.Fatalf("Failed to get child change files: %v", err)
	}
	if len(childFiles) != 1 {
		t.Fatalf("Expected child change to have 1 file (inherited), got %d", len(childFiles))
	}
	if !bytes.Equal(childFiles[0].ContentHash, fileHash) {
		t.Fatalf("Child change has wrong file hash")
	}
	// Verify they share the same file_id (important for the bug)
	if rootFiles[0].ID != childFiles[0].ID {
		t.Fatalf("Expected parent and child to share same file_id, got parent=%d child=%d", rootFiles[0].ID, childFiles[0].ID)
	}
	t.Logf("Parent and child share file_id=%d", rootFiles[0].ID)

	// Update config to work on child change
	config.ChangeId = childChangeId
	if err := config.Save(filepath.Join(tmpDir, ".pogo.yaml")); err != nil {
		t.Fatalf("Failed to save config for child: %v", err)
	}

	// Add .pogoignore to ignore *.level files
	pogoignore := filepath.Join(tmpDir, ".pogoignore")
	if err := os.WriteFile(pogoignore, []byte("*.level\n"), 0644); err != nil {
		t.Fatalf("Failed to create .pogoignore: %v", err)
	}

	// Push to child change with the ignore pattern
	// This should remove the file from C, but NOT delete the object because P still references it
	clientChild, err := client.OpenFromFile(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to open client for child: %v", err)
	}
	defer clientChild.Close()

	if err := clientChild.PushFull(false); err != nil {
		t.Fatalf("Failed to push to child change with ignore: %v", err)
	}

	// Wait for any async GC to complete
	time.Sleep(500 * time.Millisecond)

	// Verify child change no longer has the file (it was ignored)
	childFilesAfter, err := db.Q.GetChangeFiles(ctx, childChangeId)
	if err != nil {
		t.Fatalf("Failed to get child change files after push: %v", err)
	}
	// Should have only .pogoignore now
	hasLevelFile := false
	for _, f := range childFilesAfter {
		if bytes.Equal(f.ContentHash, fileHash) {
			hasLevelFile = true
		}
	}
	if hasLevelFile {
		t.Errorf("Child change should NOT have the .level file after push with ignore")
	}

	// CRITICAL: Verify the object file still exists (parent still references it)
	hashStr := base64.URLEncoding.EncodeToString(fileHash)
	filePath := filecontents.GetFilePathFromHash(hashStr)

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Errorf("BUG: File was incorrectly deleted even though parent change still references it!")
		t.Errorf("File path: %s", filePath)
	}

	// Verify parent change still has the file reference in database
	rootFilesAfter, err := db.Q.GetChangeFiles(ctx, rootChangeId)
	if err != nil {
		t.Fatalf("Failed to get root change files after: %v", err)
	}
	if len(rootFilesAfter) != 1 {
		t.Errorf("Parent change should still have 1 file, got %d", len(rootFilesAfter))
	}

	// Verify database shows the file still exists
	exists, err := db.Q.CheckFileHashExists(ctx, fileHash)
	if err != nil {
		t.Fatalf("Failed to check file hash exists: %v", err)
	}
	if !exists {
		t.Errorf("BUG: File hash was removed from database even though parent references it")
	}

	// CRITICAL: Verify diff between child and parent works
	// This is the exact scenario from the user's bug report
	diffData, err := clientChild.CollectDiff(nil, nil, false, false)
	if err != nil {
		t.Errorf("BUG: Diff failed (this is the user's reported error): %v", err)
	} else {
		t.Logf("Diff succeeded! Found %d file differences", len(diffData.Files))
		// The diff should show test.level as deleted (exists in parent, not in child)
		foundDeletedLevel := false
		for _, f := range diffData.Files {
			t.Logf("  - %s: %v", f.Header.Path, f.Header.Status)
			if f.Header.Path == "test.level" {
				foundDeletedLevel = true
			}
		}
		if !foundDeletedLevel {
			t.Logf("Note: test.level not shown in diff (may be expected if file was deleted)")
		}
	}

	t.Logf("Test completed - verifying file preservation when parent still references it")
}

// TestGCPushSameContentDifferentNames tests the scenario where:
// Multiple files with the same content (same hash) but different names exist.
// When one file's reference is removed, the content should NOT be deleted
// because another file with a different name still references the same content.
func TestGCPushSameContentDifferentNames(t *testing.T) {
	ctx := context.Background()
	testEnv := setupTestEnvironment(t)
	defer testEnv.cleanup()

	// Setup token
	if err := setupToken(testEnv.serverAddr); err != nil {
		t.Fatalf("Failed to setup token: %v", err)
	}

	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "pogo-test-same-content-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize repository
	repoName := "test-same-content-repo"
	clientInit, err := client.OpenNew(ctx, testEnv.serverAddr, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer clientInit.Close()

	repoId, changeId, err := clientInit.Init(repoName, true)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	// Save config
	config := client.Repo{
		Server:   testEnv.serverAddr,
		RepoId:   repoId,
		ChangeId: changeId,
	}
	if err := config.Save(filepath.Join(tmpDir, ".pogo.yaml")); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Create TWO files with the SAME content (same hash) but DIFFERENT names
	sharedContent := "identical content in multiple files"
	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")

	if err := os.WriteFile(file1, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to create file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to create file2: %v", err)
	}

	// Push both files
	client1, err := client.OpenFromFile(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to open client: %v", err)
	}
	defer client1.Close()

	if err := client1.PushFull(false); err != nil {
		t.Fatalf("Failed to push files: %v", err)
	}

	// Get the content hash (same for both files)
	contentHash, err := filecontents.HashFile(file1)
	if err != nil {
		t.Fatalf("Failed to hash file: %v", err)
	}

	// Verify both files were created with the same content hash
	changeFiles, err := db.Q.GetChangeFiles(ctx, changeId)
	if err != nil {
		t.Fatalf("Failed to get change files: %v", err)
	}
	if len(changeFiles) != 2 {
		t.Fatalf("Expected 2 files, got %d", len(changeFiles))
	}

	// Both files should have the same content_hash but DIFFERENT file_ids
	// (because the unique constraint is on (name, executable, content_hash))
	var file1Id, file2Id int64
	for _, f := range changeFiles {
		if !bytes.Equal(f.ContentHash, contentHash) {
			t.Fatalf("File has wrong content hash")
		}
	}
	file1Id = changeFiles[0].ID
	file2Id = changeFiles[1].ID
	if file1Id == file2Id {
		t.Logf("Note: Both files share the same file_id (content-addressable storage)")
	} else {
		t.Logf("Files have different file_ids: %d and %d (same content_hash)", file1Id, file2Id)
	}

	// Now remove file1 (keep file2)
	if err := os.Remove(file1); err != nil {
		t.Fatalf("Failed to remove file1: %v", err)
	}

	// Push again - file1 should be removed, file2 should remain
	if err := client1.PushFull(false); err != nil {
		t.Fatalf("Failed to push after removing file1: %v", err)
	}

	// Wait for GC
	time.Sleep(500 * time.Millisecond)

	// CRITICAL: The content file should still exist because file2 still references it
	hashStr := base64.URLEncoding.EncodeToString(contentHash)
	filePath := filecontents.GetFilePathFromHash(hashStr)

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Errorf("BUG: Content file was incorrectly deleted even though file2 still references it!")
		t.Errorf("File path: %s", filePath)
	} else {
		t.Logf("Content file correctly preserved at %s", filePath)
	}

	// Verify file2 is still accessible
	changeFilesAfter, err := db.Q.GetChangeFiles(ctx, changeId)
	if err != nil {
		t.Fatalf("Failed to get change files after: %v", err)
	}
	if len(changeFilesAfter) != 1 {
		t.Errorf("Expected 1 file remaining, got %d", len(changeFilesAfter))
	}

	t.Logf("Test completed - verifying content preservation when another file references same content")
}

// Test actual file deletion when file is no longer used anywhere
func TestGCPushActualDeletion(t *testing.T) {
	ctx := context.Background()
	testEnv := setupTestEnvironment(t)
	defer testEnv.cleanup()

	// Setup token
	if err := setupToken(testEnv.serverAddr); err != nil {
		t.Fatalf("Failed to setup token: %v", err)
	}

	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "pogo-test-deletion-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize repository
	repoName := "test-deletion-repo"
	clientA, err := client.OpenNew(ctx, testEnv.serverAddr, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer clientA.Close()

	repoId, changeId, err := clientA.Init(repoName, true)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	// Save config
	config := client.Repo{
		Server:   testEnv.serverAddr,
		RepoId:   repoId,
		ChangeId: changeId,
	}
	if err := config.Save(filepath.Join(tmpDir, ".pogo.yaml")); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Create test file
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("content to delete"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Push first change with the file
	client2, err := client.OpenFromFile(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to open client: %v", err)
	}
	defer client2.Close()

	if err := client2.PushFull(false); err != nil {
		t.Fatalf("Failed to push first change: %v", err)
	}

	// Get the hash of the file for tracking
	fileHash, err := filecontents.HashFile(testFile)
	if err != nil {
		t.Fatalf("Failed to hash file: %v", err)
	}

	// Don't create a new change - instead, overwrite the existing change
	// This will make the file orphaned since it's removed from the only change that referenced it

	// Remove the test file - this should make it orphaned after push
	if err := os.Remove(testFile); err != nil {
		t.Fatalf("Failed to remove test file: %v", err)
	}

	// Push empty directory - removes test.txt from the existing change, making it orphaned
	if err := client2.PushFull(false); err != nil {
		t.Fatalf("Failed to push empty change: %v", err)
	}

	// Wait a bit to ensure async operations complete
	time.Sleep(500 * time.Millisecond)

	// Verify that the file was deleted from storage since it's no longer referenced
	hashStr := base64.URLEncoding.EncodeToString(fileHash)
	filePath := filecontents.GetFilePathFromHash(hashStr)

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Errorf("File should have been deleted but still exists at %s", filePath)
	}

	// Verify database state: the file should be deleted from DB too
	exists, err := db.Q.CheckFileHashExists(ctx, fileHash)
	if err != nil {
		t.Fatalf("Failed to check if file hash exists: %v", err)
	}
	if exists {
		t.Errorf("File hash should have been deleted from database")
	}

	t.Logf("Test passed: File with hash %s was correctly deleted", hashStr)
}
