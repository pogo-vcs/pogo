package filecontents

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// HashFile computes the SHA-256 hash of a file at the given path and returns it as URL-safe base64.
func HashFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// StoreFile moves a file from srcPath to the permanent store, returning the hash as URL-safe base64.
func StoreFile(srcPath string) ([]byte, error) {
	hash, err := HashFile(srcPath)
	if err != nil {
		return nil, err
	}
	hashStr := base64.URLEncoding.EncodeToString(hash)
	dir := filepath.Join(rootDir, hashStr[:2])
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	finalPath := filepath.Join(dir, hashStr[2:])

	// Try rename first (fastest if on same filesystem)
	if err := os.Rename(srcPath, finalPath); err != nil {
		// If rename fails (e.g., cross-device on Unix or file locked on Windows),
		// fall back to copy and delete
		if err := copyFile(srcPath, finalPath); err != nil {
			return nil, fmt.Errorf("copy file to store: %w", err)
		}
		// Try to remove the source file, but don't fail if we can't
		_ = os.Remove(srcPath)
	}
	return hash, nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	// Sync to ensure data is written to disk before returning
	return dstFile.Sync()
}

// MoveAllFiles moves all files from a temp dir to the permanent store, returns a map of relPath to hash.
func MoveAllFiles(tempDir string) (map[string][]byte, error) {
	result := make(map[string][]byte)
	err := filepath.Walk(tempDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(tempDir, path)
		if err != nil {
			return err
		}
		hash, err := StoreFile(path)
		if err != nil {
			return err
		}
		result[rel] = hash
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// GetFilePathFromHash returns the file path for a given base64 hash
func GetFilePathFromHash(hash string) string {
	return filepath.Join(rootDir, hash[:2], hash[2:])
}

// OpenFileByHashWithMime opens a file by its hash and returns a reader and content type
func OpenFileByHashWithMime(hash string) (io.ReadCloser, string, error) {
	filePath := GetFilePathFromHash(hash)
	file, err := os.Open(filePath)
	if err != nil {
		return nil, "", err
	}

	// Read first 512 bytes to detect content type
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		file.Close()
		return nil, "", err
	}

	// Reset file to beginning
	if _, err := file.Seek(0, 0); err != nil {
		file.Close()
		return nil, "", err
	}

	// Detect content type using http.DetectContentType
	contentType := "application/octet-stream"
	if n > 0 {
		contentType = http.DetectContentType(buffer[:n])
	}

	return file, contentType, nil
}

// OpenFileByHash opens a file by its hash and returns a reader
func OpenFileByHash(hash string) (io.ReadCloser, error) {
	filePath := GetFilePathFromHash(hash)
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}

	return file, nil
}

// OpenFileByHashWithType opens a file by its hash and returns a reader and content type
func OpenFileByHashWithType(hash string) (io.ReadCloser, FileType, error) {
	filePath := GetFilePathFromHash(hash)
	t, err := DetectFileType(filePath)
	if err != nil {
		return nil, t, errors.Join(fmt.Errorf("detect file type %s", filePath), err)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, FileType{}, errors.Join(fmt.Errorf("open file %s", filePath), err)
	}

	return file, t, nil
}
