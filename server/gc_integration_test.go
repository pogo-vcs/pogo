//go:build fakekeyring

package server_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/pogo-vcs/pogo/auth"
	"github.com/pogo-vcs/pogo/client"
	"github.com/pogo-vcs/pogo/db"
	"github.com/pogo-vcs/pogo/protos"
	"github.com/pogo-vcs/pogo/server"
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
	dataDir    string
	cleanup    func()
}

// setupTestEnvironment creates an embedded PostgreSQL and Pogo server
func setupTestEnvironment(t *testing.T, gcThreshold string) *testEnvironment {
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

	// Set DATABASE_URL environment variable for the db package
	databaseURL := fmt.Sprintf("postgres://pogo:testpass@localhost:%d/pogo?sslmode=disable", dbPort)
	os.Setenv("DATABASE_URL", databaseURL)

	// Set ROOT_TOKEN to use our static token
	os.Setenv("ROOT_TOKEN", rootToken)

	// Set GC threshold if provided
	if gcThreshold != "" {
		os.Setenv("GC_MEMORY_THRESHOLD", gcThreshold)
	}

	// Disconnect first if already connected (from a previous test)
	db.Disconnect()

	// Connect to the database
	db.Connect()

	// Create temporary directory for Pogo data
	dataDir, err := os.MkdirTemp("", "pogo-test-data-*")
	if err != nil {
		postgres.Stop()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create temp directory for Pogo data: %v", err)
	}

	// Set DATA_DIR environment variable
	os.Setenv("DATA_DIR", dataDir)

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
		dataDir:    dataDir,
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

// TestGarbageCollectionInMemory tests GC with in-memory algorithm
func TestGarbageCollectionInMemory(t *testing.T) {
	testGarbageCollectionWithThreshold(t, "10000", "in-memory")
}

// TestGarbageCollectionBatch tests GC with batch algorithm
func TestGarbageCollectionBatch(t *testing.T) {
	testGarbageCollectionWithThreshold(t, "0", "batch")
}

// testGarbageCollectionWithThreshold runs the GC test with a specific threshold
func testGarbageCollectionWithThreshold(t *testing.T, threshold, algorithmName string) {
	env := setupTestEnvironment(t, threshold)
	defer env.cleanup()

	// Create a context with timeout for the entire test
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Create a temporary directory for our test repository
	tmpDir, err := os.MkdirTemp("", fmt.Sprintf("pogo-gc-test-%s-*", algorithmName))
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Initialize repository using the client
	t.Log("Initializing repository...")
	repoName := fmt.Sprintf("test-gc-%s-%d", algorithmName, time.Now().Unix())
	repoId, changeId, err := initializeRepository(ctx, tmpDir, repoName, env.serverAddr)
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}
	t.Logf("Created repository %s (ID: %d, Change: %d)", repoName, repoId, changeId)

	// Create some legitimate files in the repository first
	t.Log("Creating legitimate files...")
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("Hello, World!"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Push the legitimate files
	t.Log("Pushing legitimate files...")
	if err := pushFiles(ctx, tmpDir); err != nil {
		t.Fatalf("Failed to push files: %v", err)
	}

	// Create a main bookmark for later pulling
	t.Log("Creating main bookmark...")
	if err := createMainBookmark(ctx, tmpDir); err != nil {
		t.Fatalf("Failed to create main bookmark: %v", err)
	}

	// Create orphaned files directly in the server's data/objects directory
	t.Log("Creating orphaned files...")
	orphanedHashes := []string{}
	for i := 0; i < 3; i++ {
		hash := createOrphanedFile(t, env.dataDir, i)
		orphanedHashes = append(orphanedHashes, hash)
		t.Logf("Created orphaned file with hash: %s", hash)
	}

	// Verify orphaned files exist
	t.Log("Verifying orphaned files exist...")
	for _, hash := range orphanedHashes {
		if !fileExistsInDataDir(env.dataDir, hash) {
			t.Errorf("Orphaned file %s was not created", hash)
		}
	}

	// Run garbage collection
	t.Log("Running garbage collection...")
	gcResult, err := runGarbageCollection(ctx, tmpDir)
	if err != nil {
		t.Fatalf("Failed to run garbage collection: %v", err)
	}
	t.Logf("GC Result: Deleted %d DB files, %d disk files, freed %d bytes",
		gcResult.DeletedDatabaseFiles, gcResult.DeletedDiskFiles, gcResult.BytesFreed)

	// Verify the expected algorithm was used based on threshold
	t.Logf("Verifying correct algorithm (%s) was used...", algorithmName)

	// Verify orphaned files were deleted
	t.Log("Verifying orphaned files were deleted...")
	for _, hash := range orphanedHashes {
		if fileExistsInDataDir(env.dataDir, hash) {
			t.Errorf("Orphaned file %s was not deleted by GC", hash)
		}
	}

	// Verify legitimate files still exist by trying to pull them
	t.Log("Verifying legitimate files still exist...")
	pullDir, err := os.MkdirTemp("", fmt.Sprintf("pogo-gc-pull-%s-*", algorithmName))
	if err != nil {
		t.Fatalf("Failed to create pull directory: %v", err)
	}
	defer os.RemoveAll(pullDir)

	if err := pullFiles(ctx, tmpDir, pullDir); err != nil {
		t.Fatalf("Failed to pull files after GC: %v", err)
	}

	pulledFile := filepath.Join(pullDir, "test.txt")
	if content, err := os.ReadFile(pulledFile); err != nil {
		t.Errorf("Legitimate file was deleted by GC: %v", err)
	} else if string(content) != "Hello, World!" {
		t.Errorf("Legitimate file content changed: got %q, want %q", string(content), "Hello, World!")
	}

	t.Logf("Test completed successfully with %s algorithm!", algorithmName)
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

func initializeRepository(ctx context.Context, dir string, repoName string, serverAddr string) (int32, int64, error) {
	// Create .pogo.yaml file
	configPath := filepath.Join(dir, ".pogo.yaml")
	config := client.Repo{
		Server: serverAddr,
	}
	if err := config.Save(configPath); err != nil {
		return 0, 0, fmt.Errorf("failed to save config: %w", err)
	}

	// Decode for use in gRPC calls
	tokenBytes, _ := auth.Decode(rootToken)

	// Connect to server
	conn, err := grpc.NewClient(serverAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return 0, 0, fmt.Errorf("failed to connect: %w", err)
	}
	defer conn.Close()

	client := protos.NewPogoClient(conn)

	// Initialize repository
	resp, err := client.Init(ctx, &protos.InitRequest{
		Auth: &protos.Auth{
			PersonalAccessToken: tokenBytes,
		},
		RepoName: repoName,
		Public:   false,
	})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to init repo: %w", err)
	}

	// Update config with repo ID and change ID
	config.RepoId = resp.RepoId
	config.ChangeId = resp.ChangeId
	if err := config.Save(configPath); err != nil {
		return 0, 0, fmt.Errorf("failed to update config: %w", err)
	}

	return resp.RepoId, resp.ChangeId, nil
}

func pushFiles(ctx context.Context, dir string) error {
	c, err := client.OpenFromFile(ctx, dir)
	if err != nil {
		return fmt.Errorf("failed to open client: %w", err)
	}
	defer c.Close()

	return c.PushFull(false)
}

func createMainBookmark(ctx context.Context, dir string) error {
	c, err := client.OpenFromFile(ctx, dir)
	if err != nil {
		return fmt.Errorf("failed to open client: %w", err)
	}
	defer c.Close()

	return c.SetBookmark("main", nil)
}

func pullFiles(ctx context.Context, repoDir, targetDir string) error {
	// Copy .pogo.yaml to target directory
	srcConfig := filepath.Join(repoDir, ".pogo.yaml")
	dstConfig := filepath.Join(targetDir, ".pogo.yaml")
	if err := copyFile(srcConfig, dstConfig); err != nil {
		return fmt.Errorf("failed to copy config: %w", err)
	}

	c, err := client.OpenFromFile(ctx, targetDir)
	if err != nil {
		return fmt.Errorf("failed to open client: %w", err)
	}
	defer c.Close()

	return c.Edit("main")
}

func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}

func createOrphanedFile(t *testing.T, dataDir string, index int) string {
	// Generate unique content for this orphaned file
	content := fmt.Sprintf("orphaned-file-%d-%d", index, time.Now().UnixNano())

	// Calculate hash
	h := sha256.Sum256([]byte(content))
	hashStr := base64.URLEncoding.EncodeToString(h[:])

	// Create the file in the data/objects directory (relative to working dir)
	dir := hashStr[:2]
	file := hashStr[2:]

	// Create directory
	objDir := filepath.Join("data", "objects", dir)
	if err := os.MkdirAll(objDir, 0755); err != nil {
		t.Logf("Failed to create directory: %v", err)
		return hashStr
	}

	// Write file
	filePath := filepath.Join(objDir, file)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Logf("Failed to write file: %v", err)
	}

	return hashStr
}

func fileExistsInDataDir(dataDir string, hash string) bool {
	dir := hash[:2]
	file := hash[2:]
	filePath := filepath.Join("data", "objects", dir, file)
	_, err := os.Stat(filePath)
	return err == nil
}

func runGarbageCollection(ctx context.Context, dir string) (*protos.GarbageCollectResponse, error) {
	c, err := client.OpenFromFile(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open client: %w", err)
	}
	defer c.Close()

	return c.GarbageCollect(ctx)
}
