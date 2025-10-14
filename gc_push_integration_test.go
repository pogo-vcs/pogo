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
