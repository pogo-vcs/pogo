//go:build fakekeyring

package main_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/db"
	"github.com/pogo-vcs/pogo/filecontents"
	"github.com/pogo-vcs/pogo/server"
	"github.com/pogo-vcs/pogo/server/env"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	rootToken = "HP9X+pubni2ufsXTeDreWsxcY+MyxFHBgM+py1hWOks="
)

type testEnvironment struct {
	postgres   *embeddedpostgres.EmbeddedPostgres
	server     *server.Server
	serverAddr string
	dbPort     uint32
	serverPort uint32
	cleanup    func()
}

// setupTestEnvironment creates an embedded PostgreSQL and Pogo server
func setupTestEnvironment(t *testing.T) *testEnvironment {
	t.Helper()
	ctx := context.Background()

	// Find free ports for PostgreSQL and Pogo server
	dbPort, err := getFreePort()
	if err != nil {
		t.Fatalf("Failed to get free port for PostgreSQL: %v", err)
	}

	serverPort, err := getFreePort()
	if err != nil {
		t.Fatalf("Failed to get free port for Pogo server: %v", err)
	}

	// Create temporary directory for PostgreSQL data
	tmpDir, err := os.MkdirTemp("", "pogo-test-postgres-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory for PostgreSQL: %v", err)
	}

	// Create runtime directory for embedded PostgreSQL
	runtimeDir := filepath.Join(tmpDir, "runtime")
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create runtime directory: %v", err)
	}

	// Start embedded PostgreSQL with isolated runtime path
	postgresConfig := embeddedpostgres.DefaultConfig().
		Port(dbPort).
		DataPath(filepath.Join(tmpDir, "data")).
		Database("pogo").
		Username("pogo").
		Password("testpass").
		Version(embeddedpostgres.V16).
		StartTimeout(30 * time.Second).
		RuntimePath(runtimeDir)

	postgres := embeddedpostgres.NewDatabase(postgresConfig)

	if err := postgres.Start(); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to start embedded PostgreSQL: %v", err)
	}

	// Create temporary directory for Pogo data
	dataDir, err := os.MkdirTemp("", "pogo-test-data-*")
	if err != nil {
		postgres.Stop()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create temp directory for Pogo data: %v", err)
	}

	// Initialize environment configuration for testing
	databaseURL := fmt.Sprintf("postgres://pogo:testpass@localhost:%d/pogo?sslmode=disable", dbPort)
	publicAddress := fmt.Sprintf("http://localhost:%d", serverPort)
	envConfig := env.Config{
		DatabaseUrl:       databaseURL,
		PublicAddress:     publicAddress,
		RootToken:         rootToken,
		ListenAddress:     fmt.Sprintf(":%d", serverPort),
		GcMemoryThreshold: 10000000,
	}
	if err := env.InitFromConfig(envConfig); err != nil {
		postgres.Stop()
		os.RemoveAll(tmpDir)
		os.RemoveAll(dataDir)
		t.Fatalf("Failed to initialize env config: %v", err)
	}

	// Set DATA_DIR environment variable
	os.Setenv("DATA_DIR", dataDir)

	// Disconnect first if already connected (from a previous test)
	db.Disconnect()

	// Connect to the database
	db.Connect()

	// Start embedded Pogo server
	srv := server.NewServer()
	serverAddr := fmt.Sprintf("localhost:%d", serverPort)

	if err := srv.Start(serverAddr); err != nil {
		postgres.Stop()
		os.RemoveAll(tmpDir)
		os.RemoveAll(dataDir)
		t.Fatalf("Failed to start Pogo server: %v", err)
	}

	// Wait for server to be ready
	if err := waitForServer(ctx, serverAddr); err != nil {
		srv.Stop(ctx)
		postgres.Stop()
		os.RemoveAll(tmpDir)
		os.RemoveAll(dataDir)
		t.Fatalf("Server failed to become ready: %v", err)
	}

	// Setup token for authentication
	if err := setupToken(serverAddr); err != nil {
		srv.Stop(ctx)
		postgres.Stop()
		os.RemoveAll(tmpDir)
		os.RemoveAll(dataDir)
		t.Fatalf("Failed to setup token: %v", err)
	}

	return &testEnvironment{
		postgres:   postgres,
		server:     srv,
		serverAddr: serverAddr,
		dbPort:     dbPort,
		serverPort: serverPort,
		cleanup: func() {
			// Stop server
			ctx := context.Background()
			srv.Stop(ctx)

			// Disconnect from database
			db.Disconnect()

			// Stop PostgreSQL
			postgres.Stop()

			// Clean up directories
			os.RemoveAll(tmpDir)
			os.RemoveAll(dataDir)
		},
	}
}

// getFreePort finds a free port to use
func getFreePort() (uint32, error) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	return uint32(addr.Port), nil
}

// TestPogoIntegration tests all basic Pogo operations
func TestPogoIntegration(t *testing.T) {
	env := setupTestEnvironment(t)
	defer env.cleanup()

	// Create a context with timeout for the entire test
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Run all test scenarios
	t.Run("BasicOperations", func(t *testing.T) {
		testBasicOperations(t, ctx, env.serverAddr)
	})

	t.Run("MultipleChanges", func(t *testing.T) {
		testMultipleChanges(t, ctx, env.serverAddr)
	})

	t.Run("Merges", func(t *testing.T) {
		testMerges(t, ctx, env.serverAddr)
	})

	t.Run("MergeConflicts", func(t *testing.T) {
		testMergeConflicts(t, ctx, env.serverAddr)
	})

	t.Run("BinaryFiles", func(t *testing.T) {
		testBinaryFiles(t, ctx, env.serverAddr)
	})

	t.Run("Bookmarks", func(t *testing.T) {
		testBookmarks(t, ctx, env.serverAddr)
	})

	t.Run("ReadonlyChanges", func(t *testing.T) {
		testReadonlyChanges(t, ctx, env.serverAddr)
	})
}

// Test basic operations: init, push, pull
func testBasicOperations(t *testing.T, ctx context.Context, serverAddr string) {
	// Create a temporary directory for our test repository
	tmpDir, err := os.MkdirTemp("", "pogo-test-basic-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Setup token for this specific server address if not already done
	if err := setupToken(serverAddr); err != nil {
		t.Fatalf("Failed to setup token: %v", err)
	}

	// Initialize repository
	t.Log("Initializing repository...")
	repoName := fmt.Sprintf("test-basic-%d", time.Now().Unix())
	c, err := client.OpenNew(ctx, serverAddr, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	repoId, changeId, err := c.Init(repoName, false)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}
	t.Logf("Created repository %s (ID: %d, Initial change: %d)", repoName, repoId, changeId)

	// Save config
	config := client.Repo{
		Server:   serverAddr,
		RepoId:   repoId,
		ChangeId: changeId,
	}
	if err := config.Save(filepath.Join(tmpDir, ".pogo.yaml")); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Create test files
	testFiles := map[string]string{
		"README.md":     "# Test Repository\nThis is a test repository for Pogo integration testing.",
		"src/main.go":   "package main\n\nfunc main() {\n\tprintln(\"Hello, Pogo!\")\n}\n",
		"src/utils.go":  "package main\n\nfunc add(a, b int) int {\n\treturn a + b\n}\n",
		"docs/guide.md": "# User Guide\nWelcome to Pogo!",
		".gitignore":    "*.tmp\n*.log\n",
	}

	for path, content := range testFiles {
		fullPath := filepath.Join(tmpDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory for %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create file %s: %v", path, err)
		}
	}

	// Push files
	t.Log("Pushing files...")
	c2, err := client.OpenFromFile(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to open client from file: %v", err)
	}
	defer c2.Close()

	if err := c2.PushFull(false); err != nil {
		t.Fatalf("Failed to push files: %v", err)
	}
	t.Log("Files pushed successfully")

	// Create main bookmark
	if err := c2.SetBookmark("main", nil); err != nil {
		t.Fatalf("Failed to create main bookmark: %v", err)
	}

	// Pull files to a new directory
	pullDir, err := os.MkdirTemp("", "pogo-test-pull-*")
	if err != nil {
		t.Fatalf("Failed to create pull directory: %v", err)
	}
	defer os.RemoveAll(pullDir)

	// Copy config to pull directory
	if err := copyFile(filepath.Join(tmpDir, ".pogo.yaml"), filepath.Join(pullDir, ".pogo.yaml")); err != nil {
		t.Fatalf("Failed to copy config: %v", err)
	}

	// Pull files
	t.Log("Pulling files...")
	c3, err := client.OpenFromFile(ctx, pullDir)
	if err != nil {
		t.Fatalf("Failed to open client for pull: %v", err)
	}
	defer c3.Close()

	if err := c3.Edit("main"); err != nil {
		t.Fatalf("Failed to pull files: %v", err)
	}

	// Verify pulled files
	for path, expectedContent := range testFiles {
		fullPath := filepath.Join(pullDir, path)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("Failed to read pulled file %s: %v", path, err)
			continue
		}
		if string(content) != expectedContent {
			t.Errorf("Content mismatch for %s:\nExpected: %q\nGot: %q", path, expectedContent, string(content))
		}
	}
	t.Log("All files pulled and verified successfully")
}

// Test creating multiple changes
func testMultipleChanges(t *testing.T, ctx context.Context, serverAddr string) {
	tmpDir, err := os.MkdirTemp("", "pogo-test-changes-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize repository
	repoName := fmt.Sprintf("test-changes-%d", time.Now().Unix())
	c, err := client.OpenNew(ctx, serverAddr, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	repoId, changeId, err := c.Init(repoName, false)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	// Save config
	config := client.Repo{
		Server:   serverAddr,
		RepoId:   repoId,
		ChangeId: changeId,
	}
	if err := config.Save(filepath.Join(tmpDir, ".pogo.yaml")); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Create and push initial file
	if err := os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("Initial content"), 0644); err != nil {
		t.Fatalf("Failed to create file1.txt: %v", err)
	}

	c2, err := client.OpenFromFile(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to open client: %v", err)
	}
	defer c2.Close()

	if err := c2.PushFull(false); err != nil {
		t.Fatalf("Failed to push initial files: %v", err)
	}

	// Store first change name by getting info
	info1, err := c2.Info()
	if err != nil {
		t.Fatalf("Failed to get info: %v", err)
	}
	firstChangeName := info1.ChangeName
	t.Logf("First change: %s", firstChangeName)

	// Create second change
	description := "Second change with new file"
	newChangeId, changeName2, err := c2.NewChange(&description, []string{firstChangeName})
	if err != nil {
		t.Fatalf("Failed to create new change: %v", err)
	}
	c2.ConfigSetChangeId(newChangeId)
	t.Logf("Created second change: %s (ID: %d)", changeName2, newChangeId)

	// Add second file
	if err := os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("Second file"), 0644); err != nil {
		t.Fatalf("Failed to create file2.txt: %v", err)
	}

	if err := c2.PushFull(false); err != nil {
		t.Fatalf("Failed to push second change: %v", err)
	}

	// Create third change
	description3 := "Third change with modifications"
	newChangeId3, changeName3, err := c2.NewChange(&description3, []string{changeName2})
	if err != nil {
		t.Fatalf("Failed to create third change: %v", err)
	}
	c2.ConfigSetChangeId(newChangeId3)
	t.Logf("Created third change: %s (ID: %d)", changeName3, newChangeId3)

	// Modify first file
	if err := os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("Modified content in third change"), 0644); err != nil {
		t.Fatalf("Failed to modify file1.txt: %v", err)
	}

	if err := c2.PushFull(false); err != nil {
		t.Fatalf("Failed to push third change: %v", err)
	}

	// Verify we can switch between changes
	t.Log("Testing change switching...")

	// Switch to first change
	if err := c2.Edit(firstChangeName); err != nil {
		t.Fatalf("Failed to switch to first change: %v", err)
	}

	// Verify first change state
	content1, err := os.ReadFile(filepath.Join(tmpDir, "file1.txt"))
	if err != nil {
		t.Fatalf("Failed to read file1.txt in first change: %v", err)
	}
	if string(content1) != "Initial content" {
		t.Errorf("Unexpected content in first change: got %q, want %q", string(content1), "Initial content")
	}

	// file2.txt should not exist in first change
	if _, err := os.Stat(filepath.Join(tmpDir, "file2.txt")); !os.IsNotExist(err) {
		t.Error("file2.txt should not exist in first change")
	}

	// Switch to third change
	if err := c2.Edit(changeName3); err != nil {
		t.Fatalf("Failed to switch to third change: %v", err)
	}

	// Verify third change state
	content3, err := os.ReadFile(filepath.Join(tmpDir, "file1.txt"))
	if err != nil {
		t.Fatalf("Failed to read file1.txt in third change: %v", err)
	}
	if string(content3) != "Modified content in third change" {
		t.Errorf("Unexpected content in third change: got %q, want %q", string(content3), "Modified content in third change")
	}

	content2, err := os.ReadFile(filepath.Join(tmpDir, "file2.txt"))
	if err != nil {
		t.Fatalf("Failed to read file2.txt in third change: %v", err)
	}
	if string(content2) != "Second file" {
		t.Errorf("Unexpected content for file2.txt: got %q, want %q", string(content2), "Second file")
	}

	t.Log("Multiple changes test completed successfully")
}

// Test merging changes
func testMerges(t *testing.T, ctx context.Context, serverAddr string) {
	tmpDir, err := os.MkdirTemp("", "pogo-test-merge-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize repository
	repoName := fmt.Sprintf("test-merge-%d", time.Now().Unix())
	c, err := client.OpenNew(ctx, serverAddr, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	repoId, changeId, err := c.Init(repoName, false)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	config := client.Repo{
		Server:   serverAddr,
		RepoId:   repoId,
		ChangeId: changeId,
	}
	if err := config.Save(filepath.Join(tmpDir, ".pogo.yaml")); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Create base file
	if err := os.WriteFile(filepath.Join(tmpDir, "base.txt"), []byte("Base content\nLine 2\nLine 3"), 0644); err != nil {
		t.Fatalf("Failed to create base.txt: %v", err)
	}

	c2, err := client.OpenFromFile(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to open client: %v", err)
	}
	defer c2.Close()

	if err := c2.PushFull(false); err != nil {
		t.Fatalf("Failed to push base: %v", err)
	}

	info, err := c2.Info()
	if err != nil {
		t.Fatalf("Failed to get info: %v", err)
	}
	baseName := info.ChangeName
	t.Logf("Base change: %s", baseName)

	// Create branch A
	descA := "Branch A - add file"
	changeIdA, nameA, err := c2.NewChange(&descA, []string{baseName})
	if err != nil {
		t.Fatalf("Failed to create branch A: %v", err)
	}
	c2.ConfigSetChangeId(changeIdA)
	t.Logf("Created branch A: %s", nameA)

	if err := os.WriteFile(filepath.Join(tmpDir, "fileA.txt"), []byte("File from branch A"), 0644); err != nil {
		t.Fatalf("Failed to create fileA.txt: %v", err)
	}
	if err := c2.PushFull(false); err != nil {
		t.Fatalf("Failed to push branch A: %v", err)
	}

	// Switch back to base to create branch B
	if err := c2.Edit(baseName); err != nil {
		t.Fatalf("Failed to switch to base: %v", err)
	}

	// Create branch B
	descB := "Branch B - add different file"
	changeIdB, nameB, err := c2.NewChange(&descB, []string{baseName})
	if err != nil {
		t.Fatalf("Failed to create branch B: %v", err)
	}
	c2.ConfigSetChangeId(changeIdB)
	t.Logf("Created branch B: %s", nameB)

	if err := os.WriteFile(filepath.Join(tmpDir, "fileB.txt"), []byte("File from branch B"), 0644); err != nil {
		t.Fatalf("Failed to create fileB.txt: %v", err)
	}
	if err := c2.PushFull(false); err != nil {
		t.Fatalf("Failed to push branch B: %v", err)
	}

	// Create merge of A and B
	descMerge := "Merge branches A and B"
	changeIdMerge, nameMerge, err := c2.NewChange(&descMerge, []string{nameA, nameB})
	if err != nil {
		t.Fatalf("Failed to create merge: %v", err)
	}
	c2.ConfigSetChangeId(changeIdMerge)
	t.Logf("Created merge: %s", nameMerge)

	// Edit the merge to get the merged files
	if err := c2.Edit(nameMerge); err != nil {
		t.Fatalf("Failed to edit merge: %v", err)
	}

	// Push to save the merge
	if err := c2.PushFull(false); err != nil {
		t.Fatalf("Failed to push merge: %v", err)
	}

	// Check fileA.txt exists
	contentA, err := os.ReadFile(filepath.Join(tmpDir, "fileA.txt"))
	if err != nil {
		t.Fatalf("Failed to read fileA.txt in merge: %v", err)
	}
	if string(contentA) != "File from branch A" {
		t.Errorf("Unexpected content for fileA.txt: got %q", string(contentA))
	}

	// Check fileB.txt exists
	contentB, err := os.ReadFile(filepath.Join(tmpDir, "fileB.txt"))
	if err != nil {
		t.Fatalf("Failed to read fileB.txt in merge: %v", err)
	}
	if string(contentB) != "File from branch B" {
		t.Errorf("Unexpected content for fileB.txt: got %q", string(contentB))
	}

	// Check base.txt still exists
	contentBase, err := os.ReadFile(filepath.Join(tmpDir, "base.txt"))
	if err != nil {
		t.Fatalf("Failed to read base.txt in merge: %v", err)
	}
	if string(contentBase) != "Base content\nLine 2\nLine 3" {
		t.Errorf("Unexpected content for base.txt: got %q", string(contentBase))
	}

	t.Log("Merge test completed successfully")
}

// Test merge conflicts
func testMergeConflicts(t *testing.T, ctx context.Context, serverAddr string) {
	tmpDir, err := os.MkdirTemp("", "pogo-test-conflict-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize repository
	repoName := fmt.Sprintf("test-conflict-%d", time.Now().Unix())
	c, err := client.OpenNew(ctx, serverAddr, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	repoId, changeId, err := c.Init(repoName, false)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	config := client.Repo{
		Server:   serverAddr,
		RepoId:   repoId,
		ChangeId: changeId,
	}
	if err := config.Save(filepath.Join(tmpDir, ".pogo.yaml")); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Create base file
	if err := os.WriteFile(filepath.Join(tmpDir, "conflict.txt"), []byte("Line 1\nLine 2\nLine 3"), 0644); err != nil {
		t.Fatalf("Failed to create conflict.txt: %v", err)
	}

	c2, err := client.OpenFromFile(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to open client: %v", err)
	}
	defer c2.Close()

	if err := c2.PushFull(false); err != nil {
		t.Fatalf("Failed to push base: %v", err)
	}

	info, err := c2.Info()
	if err != nil {
		t.Fatalf("Failed to get info: %v", err)
	}
	baseName := info.ChangeName
	t.Logf("Base change: %s", baseName)

	// Create branch A with modification
	descA := "Branch A - modify line 2"
	changeIdA, nameA, err := c2.NewChange(&descA, []string{baseName})
	if err != nil {
		t.Fatalf("Failed to create branch A: %v", err)
	}
	c2.ConfigSetChangeId(changeIdA)

	if err := os.WriteFile(filepath.Join(tmpDir, "conflict.txt"), []byte("Line 1\nLine 2 modified by A\nLine 3"), 0644); err != nil {
		t.Fatalf("Failed to modify conflict.txt in branch A: %v", err)
	}
	if err := c2.PushFull(false); err != nil {
		t.Fatalf("Failed to push branch A: %v", err)
	}

	// Switch back to base for branch B
	if err := c2.Edit(baseName); err != nil {
		t.Fatalf("Failed to switch to base: %v", err)
	}

	// Create branch B with different modification
	descB := "Branch B - modify line 2 differently"
	changeIdB, nameB, err := c2.NewChange(&descB, []string{baseName})
	if err != nil {
		t.Fatalf("Failed to create branch B: %v", err)
	}
	c2.ConfigSetChangeId(changeIdB)

	if err := os.WriteFile(filepath.Join(tmpDir, "conflict.txt"), []byte("Line 1\nLine 2 modified by B\nLine 3"), 0644); err != nil {
		t.Fatalf("Failed to modify conflict.txt in branch B: %v", err)
	}
	if err := c2.PushFull(false); err != nil {
		t.Fatalf("Failed to push branch B: %v", err)
	}

	// Try to create a merge - this should create a conflict
	descMerge := "Merge with conflict"
	changeIdMerge, nameMerge, err := c2.NewChange(&descMerge, []string{nameA, nameB})
	if err != nil {
		t.Fatalf("Failed to create merge with conflict: %v", err)
	}
	c2.ConfigSetChangeId(changeIdMerge)
	t.Logf("Created merge with conflict: %s", nameMerge)

	// The file should now contain conflict markers
	if err := c2.Edit(nameMerge); err != nil {
		t.Fatalf("Failed to switch to merge: %v", err)
	}

	conflictContent, err := os.ReadFile(filepath.Join(tmpDir, "conflict.txt"))
	if err != nil {
		t.Fatalf("Failed to read conflict.txt: %v", err)
	}

	// Check for conflict markers
	content := string(conflictContent)
	if !strings.Contains(content, filecontents.ConflictMarkerStart) || !strings.Contains(content, filecontents.ConflictMarkerEnd) {
		t.Logf("File content:\n%s", content)
		t.Error("Expected conflict markers not found in file")
	}

	// Resolve the conflict manually
	resolvedContent := "Line 1\nLine 2 resolved\nLine 3"
	if err := os.WriteFile(filepath.Join(tmpDir, "conflict.txt"), []byte(resolvedContent), 0644); err != nil {
		t.Fatalf("Failed to write resolved content: %v", err)
	}

	// Push the resolved conflict
	if err := c2.PushFull(false); err != nil {
		t.Fatalf("Failed to push resolved conflict: %v", err)
	}

	// Verify the resolution
	verifyContent, err := os.ReadFile(filepath.Join(tmpDir, "conflict.txt"))
	if err != nil {
		t.Fatalf("Failed to read resolved file: %v", err)
	}
	if string(verifyContent) != resolvedContent {
		t.Errorf("Unexpected resolved content: got %q, want %q", string(verifyContent), resolvedContent)
	}

	t.Log("Conflict test completed successfully")
}

// Test binary file handling
func testBinaryFiles(t *testing.T, ctx context.Context, serverAddr string) {
	tmpDir, err := os.MkdirTemp("", "pogo-test-binary-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize repository
	repoName := fmt.Sprintf("test-binary-%d", time.Now().Unix())
	c, err := client.OpenNew(ctx, serverAddr, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	repoId, changeId, err := c.Init(repoName, false)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	config := client.Repo{
		Server:   serverAddr,
		RepoId:   repoId,
		ChangeId: changeId,
	}
	if err := config.Save(filepath.Join(tmpDir, ".pogo.yaml")); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Create various binary files
	binaryFiles := map[string][]byte{
		"image.png":   generateBinaryData(1024, 0x89), // PNG-like header
		"data.bin":    generateBinaryData(2048, 0x00), // Generic binary
		"archive.zip": generateBinaryData(512, 0x50),  // ZIP-like header
	}

	for name, data := range binaryFiles {
		if err := os.WriteFile(filepath.Join(tmpDir, name), data, 0644); err != nil {
			t.Fatalf("Failed to create binary file %s: %v", name, err)
		}
	}

	// Also add a text file
	if err := os.WriteFile(filepath.Join(tmpDir, "text.txt"), []byte("Text file alongside binaries"), 0644); err != nil {
		t.Fatalf("Failed to create text.txt: %v", err)
	}

	// Push files
	c2, err := client.OpenFromFile(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to open client: %v", err)
	}
	defer c2.Close()

	if err := c2.PushFull(false); err != nil {
		t.Fatalf("Failed to push binary files: %v", err)
	}

	// Create bookmark for pulling
	if err := c2.SetBookmark("binary-test", nil); err != nil {
		t.Fatalf("Failed to create bookmark: %v", err)
	}

	// Pull to verify
	pullDir, err := os.MkdirTemp("", "pogo-test-binary-pull-*")
	if err != nil {
		t.Fatalf("Failed to create pull directory: %v", err)
	}
	defer os.RemoveAll(pullDir)

	if err := copyFile(filepath.Join(tmpDir, ".pogo.yaml"), filepath.Join(pullDir, ".pogo.yaml")); err != nil {
		t.Fatalf("Failed to copy config: %v", err)
	}

	c3, err := client.OpenFromFile(ctx, pullDir)
	if err != nil {
		t.Fatalf("Failed to open client for pull: %v", err)
	}
	defer c3.Close()

	if err := c3.Edit("binary-test"); err != nil {
		t.Fatalf("Failed to pull binary files: %v", err)
	}

	// Verify binary files
	for name, expectedData := range binaryFiles {
		pulledData, err := os.ReadFile(filepath.Join(pullDir, name))
		if err != nil {
			t.Errorf("Failed to read pulled binary file %s: %v", name, err)
			continue
		}
		if !bytesEqual(pulledData, expectedData) {
			t.Errorf("Binary file %s corrupted during push/pull", name)
		}
	}

	// Verify text file
	textContent, err := os.ReadFile(filepath.Join(pullDir, "text.txt"))
	if err != nil {
		t.Errorf("Failed to read text.txt: %v", err)
	} else if string(textContent) != "Text file alongside binaries" {
		t.Errorf("Text file corrupted: got %q", string(textContent))
	}

	t.Log("Binary files test completed successfully")
}

// Test bookmark operations
func testBookmarks(t *testing.T, ctx context.Context, serverAddr string) {
	tmpDir, err := os.MkdirTemp("", "pogo-test-bookmarks-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize repository
	repoName := fmt.Sprintf("test-bookmarks-%d", time.Now().Unix())
	c, err := client.OpenNew(ctx, serverAddr, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	repoId, changeId, err := c.Init(repoName, false)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	config := client.Repo{
		Server:   serverAddr,
		RepoId:   repoId,
		ChangeId: changeId,
	}
	if err := config.Save(filepath.Join(tmpDir, ".pogo.yaml")); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	c2, err := client.OpenFromFile(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to open client: %v", err)
	}
	defer c2.Close()

	// Create initial file and push
	if err := os.WriteFile(filepath.Join(tmpDir, "version.txt"), []byte("v1.0.0"), 0644); err != nil {
		t.Fatalf("Failed to create version.txt: %v", err)
	}
	if err := c2.PushFull(false); err != nil {
		t.Fatalf("Failed to push v1.0.0: %v", err)
	}

	info1, err := c2.Info()
	if err != nil {
		t.Fatalf("Failed to get info: %v", err)
	}
	v1Name := info1.ChangeName

	// Create v1.0.0 bookmark
	if err := c2.SetBookmark("v1.0.0", &v1Name); err != nil {
		t.Fatalf("Failed to create v1.0.0 bookmark: %v", err)
	}

	// Create v2.0.0
	desc2 := "Version 2.0.0"
	changeId2, v2Name, err := c2.NewChange(&desc2, []string{v1Name})
	if err != nil {
		t.Fatalf("Failed to create v2.0.0 change: %v", err)
	}
	c2.ConfigSetChangeId(changeId2)

	if err := os.WriteFile(filepath.Join(tmpDir, "version.txt"), []byte("v2.0.0"), 0644); err != nil {
		t.Fatalf("Failed to update version.txt to v2.0.0: %v", err)
	}
	if err := c2.PushFull(false); err != nil {
		t.Fatalf("Failed to push v2.0.0: %v", err)
	}

	// Create v2.0.0 bookmark
	if err := c2.SetBookmark("v2.0.0", &v2Name); err != nil {
		t.Fatalf("Failed to create v2.0.0 bookmark: %v", err)
	}

	// Create main bookmark pointing to latest
	if err := c2.SetBookmark("main", &v2Name); err != nil {
		t.Fatalf("Failed to create main bookmark: %v", err)
	}

	// List bookmarks
	bookmarks, err := c2.GetBookmarks()
	if err != nil {
		t.Fatalf("Failed to get bookmarks: %v", err)
	}

	// Verify bookmarks exist
	bookmarkMap := make(map[string]string)
	for _, b := range bookmarks {
		bookmarkMap[b.Name] = b.ChangeName
	}

	if bookmarkMap["v1.0.0"] != v1Name {
		t.Errorf("v1.0.0 bookmark points to wrong change: got %s, want %s", bookmarkMap["v1.0.0"], v1Name)
	}
	if bookmarkMap["v2.0.0"] != v2Name {
		t.Errorf("v2.0.0 bookmark points to wrong change: got %s, want %s", bookmarkMap["v2.0.0"], v2Name)
	}
	if bookmarkMap["main"] != v2Name {
		t.Errorf("main bookmark points to wrong change: got %s, want %s", bookmarkMap["main"], v2Name)
	}

	// Test switching via bookmarks
	if err := c2.Edit("v1.0.0"); err != nil {
		t.Fatalf("Failed to switch to v1.0.0 bookmark: %v", err)
	}

	content1, err := os.ReadFile(filepath.Join(tmpDir, "version.txt"))
	if err != nil {
		t.Fatalf("Failed to read version.txt at v1.0.0: %v", err)
	}
	if string(content1) != "v1.0.0" {
		t.Errorf("Wrong content at v1.0.0: got %q, want %q", string(content1), "v1.0.0")
	}

	if err := c2.Edit("main"); err != nil {
		t.Fatalf("Failed to switch to main bookmark: %v", err)
	}

	contentMain, err := os.ReadFile(filepath.Join(tmpDir, "version.txt"))
	if err != nil {
		t.Fatalf("Failed to read version.txt at main: %v", err)
	}
	if string(contentMain) != "v2.0.0" {
		t.Errorf("Wrong content at main: got %q, want %q", string(contentMain), "v2.0.0")
	}

	// Update bookmark
	desc3 := "Version 2.0.1 - hotfix"
	changeId3, v201Name, err := c2.NewChange(&desc3, []string{v2Name})
	if err != nil {
		t.Fatalf("Failed to create v2.0.1 change: %v", err)
	}
	c2.ConfigSetChangeId(changeId3)

	if err := os.WriteFile(filepath.Join(tmpDir, "version.txt"), []byte("v2.0.1"), 0644); err != nil {
		t.Fatalf("Failed to update version.txt to v2.0.1: %v", err)
	}
	if err := c2.PushFull(false); err != nil {
		t.Fatalf("Failed to push v2.0.1: %v", err)
	}

	// Update main bookmark
	if err := c2.SetBookmark("main", &v201Name); err != nil {
		t.Fatalf("Failed to update main bookmark: %v", err)
	}

	// Verify main now points to v2.0.1
	bookmarks2, err := c2.GetBookmarks()
	if err != nil {
		t.Fatalf("Failed to get updated bookmarks: %v", err)
	}

	for _, b := range bookmarks2 {
		if b.Name == "main" && b.ChangeName != v201Name {
			t.Errorf("main bookmark not updated: got %s, want %s", b.ChangeName, v201Name)
		}
	}

	// Test bookmark removal
	originalBookmarkCount := len(bookmarks2)
	if err := c2.RemoveBookmark("v1.0.0"); err != nil {
		t.Fatalf("Failed to remove v1.0.0 bookmark: %v", err)
	}

	// Verify bookmark was removed
	bookmarks3, err := c2.GetBookmarks()
	if err != nil {
		t.Fatalf("Failed to get bookmarks after removal: %v", err)
	}

	if len(bookmarks3) != originalBookmarkCount-1 {
		t.Errorf("Expected %d bookmarks after removal, got %d", originalBookmarkCount-1, len(bookmarks3))
	}

	for _, b := range bookmarks3 {
		if b.Name == "v1.0.0" {
			t.Errorf("v1.0.0 bookmark should have been removed but still exists")
		}
	}

	// Test removing non-existent bookmark (should not error)
	if err := c2.RemoveBookmark("non-existent-bookmark"); err != nil {
		t.Fatalf("Failed to remove non-existent bookmark: %v", err)
	}

	t.Log("Bookmarks test completed successfully")
}

// Helper functions

// Test readonly change protection
func testReadonlyChanges(t *testing.T, ctx context.Context, serverAddr string) {
	// Create a temporary directory for our test repository
	tmpDir, err := os.MkdirTemp("", "pogo-test-readonly-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize repository
	t.Log("Initializing repository...")
	repoName := fmt.Sprintf("test-readonly-%d", time.Now().Unix())
	c, err := client.OpenNew(ctx, serverAddr, tmpDir)
	if err != nil {
		t.Fatalf("Failed to open client: %v", err)
	}
	defer c.Close()

	repoId, changeId, err := c.Init(repoName, false)
	if err != nil {
		t.Fatalf("Failed to init repository: %v", err)
	}
	t.Logf("Created repository %s with ID %d, initial change %d", repoName, repoId, changeId)

	// Create initial file
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("initial content\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Push initial content
	if err := c.PushFull(false); err != nil {
		t.Fatalf("Failed to push initial content: %v", err)
	}

	// Test 1: Change with bookmark should be readonly
	t.Run("BookmarkProtection", func(t *testing.T) {
		// Set a bookmark on current change
		if err := c.SetBookmark("v1.0.0", nil); err != nil {
			t.Fatalf("Failed to set bookmark: %v", err)
		}

		// Try to push to bookmarked change (should fail)
		// Use unique content with timestamp to ensure it's new content
		uniqueContent := fmt.Sprintf("modified content at %d\n", time.Now().UnixNano())
		if err := os.WriteFile(testFile, []byte(uniqueContent), 0644); err != nil {
			t.Fatalf("Failed to modify test file: %v", err)
		}

		err := c.PushFull(false)
		if err == nil {
			t.Fatal("Expected error when pushing to bookmarked change, got nil")
		}
		if !strings.Contains(err.Error(), "readonly") {
			t.Errorf("Expected readonly error, got: %v", err)
		}

		// Force push should work
		if err := c.PushFull(true); err != nil {
			t.Fatalf("Failed to force push to bookmarked change: %v", err)
		}
	})

	// Test 2: Change with children should be readonly
	t.Run("ChildProtection", func(t *testing.T) {
		// Create a new change (child of current)
		childId, childName, err := c.NewChange(nil, []string{})
		if err != nil {
			t.Fatalf("Failed to create child change: %v", err)
		}
		t.Logf("Created child change %s (ID %d)", childName, childId)

		// Edit back to parent
		info, err := c.Info()
		if err != nil {
			t.Fatalf("Failed to get info: %v", err)
		}
		parentChange := info.ChangeName
		if err := c.Edit(parentChange); err != nil {
			t.Fatalf("Failed to edit parent change: %v", err)
		}

		// Try to push to parent (should fail because it has children)
		// Use unique content with timestamp to ensure it's new content
		uniqueContent := fmt.Sprintf("parent modified at %d\n", time.Now().UnixNano())
		if err := os.WriteFile(testFile, []byte(uniqueContent), 0644); err != nil {
			t.Fatalf("Failed to modify test file: %v", err)
		}

		err = c.PushFull(false)
		if err == nil {
			t.Fatal("Expected error when pushing to change with children, got nil")
		}
		if !strings.Contains(err.Error(), "readonly") {
			t.Errorf("Expected readonly error, got: %v", err)
		}

		// Force push should work
		if err := c.PushFull(true); err != nil {
			t.Fatalf("Failed to force push to change with children: %v", err)
		}
	})

	// Test 3: Other author's change should be readonly
	// Note: This test is difficult to implement in integration tests because
	// we would need to simulate multiple users with different tokens.
	// The server-side logic is tested, but full integration test would require
	// a more complex test setup with multiple authenticated users.
	// For now, we'll skip this specific test case.
}

func waitForServer(ctx context.Context, serverAddr string) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		conn, err := grpc.NewClient(serverAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("server failed to start within 10 seconds")
}

func setupToken(serverAddr string) error {
	// Extract port from serverAddr
	parts := strings.Split(serverAddr, ":")
	if len(parts) != 2 {
		return fmt.Errorf("invalid server address format: %s", serverAddr)
	}
	port := parts[1]

	tokenDir := filepath.Join(os.Getenv("HOME"), ".config", "pogo", "tokens")
	if err := os.MkdirAll(tokenDir, 0755); err != nil {
		return fmt.Errorf("failed to create token dir: %w", err)
	}

	// Use localhost:port as the token file name to match what the client expects
	tokenFile := filepath.Join(tokenDir, fmt.Sprintf("localhost:%s", port))
	// Write the base64-encoded token string (not the raw bytes)
	// The fake keyring will decode it when reading
	if err := os.WriteFile(tokenFile, []byte(rootToken), 0600); err != nil {
		return fmt.Errorf("failed to write token: %w", err)
	}
	return nil
}

func copyFile(src, dst string) error {
	input, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, input, 0644)
}

func generateBinaryData(size int, header byte) []byte {
	data := make([]byte, size)
	// Set header byte
	data[0] = header
	// Fill with pseudo-random data
	h := sha256.New()
	h.Write([]byte{header})
	seed := h.Sum(nil)
	for i := 1; i < size; i++ {
		data[i] = seed[i%len(seed)]
	}
	return data
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestServerSideCIContainerWithRepoContent(t *testing.T) {
	if !isDockerAvailable() {
		t.Skip("Docker not available")
	}

	testEnv := setupTestEnvironment(t)
	defer testEnv.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	tmpDir, err := os.MkdirTemp("", "pogo-test-ci-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := setupToken(testEnv.serverAddr); err != nil {
		t.Fatalf("Failed to setup token: %v", err)
	}

	repoName := fmt.Sprintf("test-ci-%d", time.Now().Unix())
	c, err := client.OpenNew(ctx, testEnv.serverAddr, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	repoId, changeId, err := c.Init(repoName, false)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}
	t.Logf("Created repository %s (ID: %d, Initial change: %d)", repoName, repoId, changeId)

	config := client.Repo{
		Server:   testEnv.serverAddr,
		RepoId:   repoId,
		ChangeId: changeId,
	}
	if err := config.Save(filepath.Join(tmpDir, ".pogo.yaml")); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	testFilePath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFilePath, []byte("Hello from CI test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	scriptPath := filepath.Join(tmpDir, "test.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho 'Script executed'\n"), 0755); err != nil {
		t.Fatalf("Failed to create test script: %v", err)
	}

	ciDir := filepath.Join(tmpDir, ".pogo", "ci")
	if err := os.MkdirAll(ciDir, 0755); err != nil {
		t.Fatalf("Failed to create CI directory: %v", err)
	}

	ciConfig := `version: 1
on:
  push:
    bookmarks: ["main"]
do:
  - container:
      image: alpine:latest
      commands:
        - echo "=== CI Container Started ==="
        - echo "Working directory:" && pwd
        - echo "Files in workspace:" && ls -la
        - echo "=== Checking test.txt ===" && test -f test.txt && echo "✓ test.txt exists" && cat test.txt
        - echo "=== Checking test.sh ===" && test -f test.sh && echo "✓ test.sh exists" && test -x test.sh && echo "✓ test.sh is executable"
        - echo "=== CI Container Success ==="
`

	ciConfigPath := filepath.Join(ciDir, "main.yaml")
	if err := os.WriteFile(ciConfigPath, []byte(ciConfig), 0644); err != nil {
		t.Fatalf("Failed to create CI config: %v", err)
	}

	c2, err := client.OpenFromFile(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to open client from file: %v", err)
	}
	defer c2.Close()

	t.Log("Pushing initial files with CI config...")
	if err := c2.PushFull(false); err != nil {
		t.Fatalf("Failed to push files: %v", err)
	}

	t.Log("Setting main bookmark (this should trigger CI)...")
	if err := c2.SetBookmark("main", nil); err != nil {
		t.Fatalf("Failed to set bookmark: %v", err)
	}

	t.Log("Waiting for CI to complete...")
	time.Sleep(15 * time.Second)

	t.Log("CI test completed successfully")
}

func isDockerAvailable() bool {
	cmd := exec.Command("docker", "version")
	return cmd.Run() == nil
}
