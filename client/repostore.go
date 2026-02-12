package client

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

const ExpectedRepoVersion = 1

type RepoStore struct {
	db       *sql.DB
	location string
}

// OpenRepoStore opens an existing repository database
func OpenRepoStore(location string) (*RepoStore, error) {
	dbPath := filepath.Join(location, ".pogo.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set synchronous mode: %w", err)
	}

	store := &RepoStore{
		db:       db,
		location: location,
	}

	// Verify version
	version, err := store.GetVersion()
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("get repository version: %w", err)
	}

	if version != ExpectedRepoVersion {
		store.Close()
		return nil, fmt.Errorf("repository database version mismatch: found version %d, required version %d", version, ExpectedRepoVersion)
	}

	return store, nil
}

// CreateRepoStore creates a new repository database
func CreateRepoStore(location, server string, repoId int32, changeId int64) (*RepoStore, error) {
	dbPath := filepath.Join(location, ".pogo.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set synchronous mode: %w", err)
	}

	// Create schema
	schema := `
		CREATE TABLE metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);

		CREATE TABLE file_cache (
			path TEXT PRIMARY KEY,
			size INTEGER NOT NULL,
			mtime_sec INTEGER NOT NULL,
			mtime_nsec INTEGER NOT NULL,
			inode INTEGER NOT NULL,
			hash BLOB NOT NULL,
			created_at INTEGER NOT NULL
		);

		CREATE INDEX idx_file_cache_lookup ON file_cache(path, size, mtime_sec, mtime_nsec, inode);
	`

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	// Insert metadata
	store := &RepoStore{
		db:       db,
		location: location,
	}

	if err := store.setMetadata("version", strconv.Itoa(ExpectedRepoVersion)); err != nil {
		store.Close()
		return nil, fmt.Errorf("set version: %w", err)
	}

	if err := store.setMetadata("server", server); err != nil {
		store.Close()
		return nil, fmt.Errorf("set server: %w", err)
	}

	if err := store.setMetadata("repo_id", strconv.Itoa(int(repoId))); err != nil {
		store.Close()
		return nil, fmt.Errorf("set repo_id: %w", err)
	}

	if err := store.setMetadata("change_id", strconv.FormatInt(changeId, 10)); err != nil {
		store.Close()
		return nil, fmt.Errorf("set change_id: %w", err)
	}

	return store, nil
}

// Close closes the database connection
func (rs *RepoStore) Close() error {
	if rs.db != nil {
		return rs.db.Close()
	}
	return nil
}

// Remove closes the database and deletes .pogo.db and related WAL/SHM files
func (rs *RepoStore) Remove() {
	rs.Close()
	dbPath := filepath.Join(rs.location, ".pogo.db")
	os.Remove(dbPath)
	os.Remove(dbPath + "-shm")
	os.Remove(dbPath + "-wal")
}

// GetVersion returns the database schema version
func (rs *RepoStore) GetVersion() (int, error) {
	var versionStr string
	err := rs.db.QueryRow("SELECT value FROM metadata WHERE key = 'version'").Scan(&versionStr)
	if err != nil {
		return 0, fmt.Errorf("read version from database: %w", err)
	}

	version, err := strconv.Atoi(versionStr)
	if err != nil {
		return 0, fmt.Errorf("parse version: %w", err)
	}

	return version, nil
}

// GetServer returns the server address
func (rs *RepoStore) GetServer() (string, error) {
	return rs.getMetadata("server")
}

// GetRepoId returns the repository ID
func (rs *RepoStore) GetRepoId() (int32, error) {
	repoIdStr, err := rs.getMetadata("repo_id")
	if err != nil {
		return 0, err
	}

	repoId, err := strconv.Atoi(repoIdStr)
	if err != nil {
		return 0, fmt.Errorf("parse repo_id: %w", err)
	}

	return int32(repoId), nil
}

// GetChangeId returns the current change ID
func (rs *RepoStore) GetChangeId() (int64, error) {
	changeIdStr, err := rs.getMetadata("change_id")
	if err != nil {
		return 0, err
	}

	changeId, err := strconv.ParseInt(changeIdStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse change_id: %w", err)
	}

	return changeId, nil
}

// SetChangeId updates the current change ID
func (rs *RepoStore) SetChangeId(changeId int64) error {
	return rs.setMetadata("change_id", strconv.FormatInt(changeId, 10))
}

// GetFileHash retrieves a cached file hash
// Returns (hash, true) if found, (nil, false) if not found
func (rs *RepoStore) GetFileHash(path string, size, mtimeSec, mtimeNsec, inode int64) ([]byte, bool) {
	var hash []byte
	err := rs.db.QueryRow(
		`SELECT hash FROM file_cache 
		 WHERE path = ? AND size = ? AND mtime_sec = ? AND mtime_nsec = ? AND inode = ?`,
		path, size, mtimeSec, mtimeNsec, inode,
	).Scan(&hash)

	if err != nil {
		return nil, false // Cache miss
	}
	return hash, true // Cache hit
}

// SetFileHash updates or inserts a file hash in the cache
func (rs *RepoStore) SetFileHash(path string, size, mtimeSec, mtimeNsec, inode int64, hash []byte) error {
	_, err := rs.db.Exec(
		`INSERT OR REPLACE INTO file_cache (path, size, mtime_sec, mtime_nsec, inode, hash, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		path, size, mtimeSec, mtimeNsec, inode, hash, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("update file cache: %w", err)
	}
	return nil
}

// DeleteFileHash removes a file from the cache
func (rs *RepoStore) DeleteFileHash(path string) error {
	_, err := rs.db.Exec("DELETE FROM file_cache WHERE path = ?", path)
	if err != nil {
		return fmt.Errorf("delete from file cache: %w", err)
	}
	return nil
}

// getMetadata retrieves a metadata value by key
func (rs *RepoStore) getMetadata(key string) (string, error) {
	var value string
	err := rs.db.QueryRow("SELECT value FROM metadata WHERE key = ?", key).Scan(&value)
	if err != nil {
		return "", fmt.Errorf("read %s from metadata: %w", key, err)
	}
	return value, nil
}

// setMetadata sets a metadata value
func (rs *RepoStore) setMetadata(key, value string) error {
	_, err := rs.db.Exec(
		"INSERT OR REPLACE INTO metadata (key, value) VALUES (?, ?)",
		key, value,
	)
	if err != nil {
		return fmt.Errorf("write %s to metadata: %w", key, err)
	}
	return nil
}
