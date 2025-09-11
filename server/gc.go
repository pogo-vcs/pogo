package server

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/pogo-vcs/pogo/db"
	"github.com/pogo-vcs/pogo/protos"
	"github.com/pogo-vcs/pogo/server/env"
)

var gcMutex sync.RWMutex

// Default memory threshold for in-memory strategy (10 million files = ~640MB)
const defaultInMemoryThreshold = 10_000_000

func (s *Server) GarbageCollect(ctx context.Context, req *protos.GarbageCollectRequest) (*protos.GarbageCollectResponse, error) {
	// Authenticate user - only authenticated users can trigger GC
	userId, err := getUserIdFromAuth(ctx, req.Auth)
	if err != nil {
		return nil, fmt.Errorf("authenticate user: %w", err)
	}
	if userId == nil {
		return nil, errors.New("authentication required for garbage collection")
	}

	// Lock to ensure exclusive access during GC
	gcMutex.Lock()
	defer gcMutex.Unlock()

	return runGarbageCollectionInternal(ctx)
}

// runGarbageCollectionInternal contains the actual GC logic
func runGarbageCollectionInternal(ctx context.Context) (*protos.GarbageCollectResponse, error) {
	// Start a database transaction for cleanup
	tx, err := db.Q.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Close()

	// Step 1: Delete unreachable files from database
	unreachableFiles, err := tx.GetUnreachableFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("get unreachable files: %w", err)
	}

	deletedDbFiles := int32(len(unreachableFiles))

	if err := tx.DeleteUnreachableFiles(ctx); err != nil {
		return nil, fmt.Errorf("delete unreachable files from database: %w", err)
	}

	// Commit database transaction
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	// Step 2: Count total files in database to decide strategy
	fileCount, err := db.Q.CountFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("count files: %w", err)
	}

	fmt.Printf("GC: Total files in database: %d\n", fileCount)

	// Step 3: Clean up orphaned files from disk using appropriate strategy
	var deletedDiskFiles int32
	var bytesFreed int64

	if int64(fileCount) < env.GcMemoryThreshold {
		// Use in-memory strategy for smaller datasets
		fmt.Printf("GC: Using in-memory strategy (threshold: %d, files: %d)\n", env.GcMemoryThreshold, fileCount)
		deletedDiskFiles, bytesFreed, err = cleanupDiskInMemory(ctx)
	} else {
		// Use batch strategy for larger datasets
		fmt.Printf("GC: Using batch strategy (threshold: %d, files: %d)\n", env.GcMemoryThreshold, fileCount)
		deletedDiskFiles, bytesFreed, err = cleanupDiskBatch(ctx)
	}

	if err != nil {
		// Non-fatal error - still return results
		fmt.Printf("warning: error during filesystem cleanup: %v\n", err)
	}

	// Try to remove empty directories
	objectsDir := filepath.Join("data", "objects")
	_ = cleanEmptyDirs(objectsDir)

	return &protos.GarbageCollectResponse{
		DeletedDatabaseFiles: deletedDbFiles,
		DeletedDiskFiles:     deletedDiskFiles,
		BytesFreed:           bytesFreed,
	}, nil
}

// cleanupDiskInMemory uses the original in-memory hash map approach for smaller datasets
func cleanupDiskInMemory(ctx context.Context) (int32, int64, error) {
	// Get all file hashes from database
	allDbHashes, err := db.Q.GetAllFileHashes(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("get all file hashes: %w", err)
	}

	// Create a map of all hashes in database
	dbHashMap := make(map[string]bool, len(allDbHashes))
	for _, hash := range allDbHashes {
		hashStr := base64.URLEncoding.EncodeToString(hash)
		dbHashMap[hashStr] = true
	}

	return walkAndCleanFiles(func(hash string) bool {
		return !dbHashMap[hash]
	})
}

// cleanupDiskBatch uses batch database queries for larger datasets
func cleanupDiskBatch(ctx context.Context) (int32, int64, error) {
	// Collect file hashes from disk in batches and check against database
	const batchSize = 1000
	var batch []string
	var deletedFiles int32
	var bytesFreed int64

	objectsDir := filepath.Join("data", "objects")

	err := filepath.WalkDir(objectsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Continue on error
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Extract hash from path
		rel, err := filepath.Rel(objectsDir, path)
		if err != nil {
			return nil
		}

		// Reconstruct hash from directory structure
		dir := filepath.Dir(rel)
		file := filepath.Base(rel)

		// Skip if not in expected structure
		if dir == "." || len(dir) != 2 {
			return nil
		}

		hash := dir + file
		batch = append(batch, hash)

		// Process batch when it's full
		if len(batch) >= batchSize {
			deleted, freed, err := processBatch(ctx, batch, objectsDir)
			if err != nil {
				fmt.Printf("warning: error processing batch: %v\n", err)
			}
			deletedFiles += deleted
			bytesFreed += freed
			batch = batch[:0] // Clear batch
		}

		return nil
	})

	// Process remaining items in batch
	if len(batch) > 0 {
		deleted, freed, err := processBatch(ctx, batch, objectsDir)
		if err != nil {
			fmt.Printf("warning: error processing final batch: %v\n", err)
		}
		deletedFiles += deleted
		bytesFreed += freed
	}

	return deletedFiles, bytesFreed, err
}

// processBatch checks a batch of file hashes against the database and deletes orphans
func processBatch(ctx context.Context, hashes []string, objectsDir string) (int32, int64, error) {
	var deletedFiles int32
	var bytesFreed int64

	// Check each hash individually against the database
	for _, hashStr := range hashes {
		hashBytes, err := base64.URLEncoding.DecodeString(hashStr)
		if err != nil {
			continue // Skip invalid hashes
		}

		exists, err := db.Q.CheckFileHashExists(ctx, hashBytes)
		if err != nil {
			continue // Skip on error
		}

		if !exists {
			// File is not in database, delete it
			dir := hashStr[:2]
			file := hashStr[2:]
			path := filepath.Join(objectsDir, dir, file)

			info, err := os.Stat(path)
			if err == nil {
				bytesFreed += info.Size()
			}

			if err := os.Remove(path); err == nil {
				deletedFiles++
			}
		}
	}

	return deletedFiles, bytesFreed, nil
}

// walkAndCleanFiles walks the filesystem and deletes files based on shouldDelete function
func walkAndCleanFiles(shouldDelete func(hash string) bool) (int32, int64, error) {
	var deletedDiskFiles int32
	var bytesFreed int64

	objectsDir := filepath.Join("data", "objects")

	err := filepath.WalkDir(objectsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Continue on error
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Extract hash from path
		rel, err := filepath.Rel(objectsDir, path)
		if err != nil {
			return nil
		}

		// Reconstruct hash from directory structure
		dir := filepath.Dir(rel)
		file := filepath.Base(rel)

		// Skip if not in expected structure
		if dir == "." || len(dir) != 2 {
			return nil
		}

		hash := dir + file

		// Check if this file should be deleted
		if shouldDelete(hash) {
			info, err := d.Info()
			if err == nil {
				bytesFreed += info.Size()
			}

			if err := os.Remove(path); err == nil {
				deletedDiskFiles++
			}
		}

		return nil
	})

	return deletedDiskFiles, bytesFreed, err
}

func cleanEmptyDirs(root string) error {
	// Walk the directory tree bottom-up to remove empty directories
	var dirs []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && path != root {
			dirs = append(dirs, path)
		}
		return nil
	})

	if err != nil {
		return err
	}

	// Process directories in reverse order (bottom-up)
	for i := len(dirs) - 1; i >= 0; i-- {
		entries, err := os.ReadDir(dirs[i])
		if err != nil {
			continue
		}
		if len(entries) == 0 {
			_ = os.Remove(dirs[i])
		}
	}

	return nil
}

// RunGarbageCollection is a helper function that can be called from cron job
func RunGarbageCollection(ctx context.Context) (*protos.GarbageCollectResponse, error) {
	// Lock to ensure exclusive access during GC
	gcMutex.Lock()
	defer gcMutex.Unlock()

	return runGarbageCollectionInternal(ctx)
}
