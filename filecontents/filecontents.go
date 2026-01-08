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
	"strings"

	"github.com/pogo-vcs/pogo/compressions"
)

var (
	ConflictMarkerStart = strings.Repeat("<", 7)
	ConflictMarkerEnd   = strings.Repeat(">", 7)
	ConflictMarkerSep   = strings.Repeat("=", 7)
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
		// fall back to copy with compression and delete
		if err := copyFileWithCompression(srcPath, finalPath); err != nil {
			return nil, fmt.Errorf("copy file to store: %w", err)
		}
		// Try to remove the source file, but don't fail if we can't
		_ = os.Remove(srcPath)
	} else {
		// File was renamed successfully, now compress it in place
		if err := compressFileInPlace(finalPath); err != nil {
			return nil, fmt.Errorf("compress file in store: %w", err)
		}
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

// copyFileWithCompression copies and compresses a file from src to dst
func copyFileWithCompression(src, dst string) error {
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

	compressedReader := compressions.Compress(srcFile)
	defer compressedReader.Close()

	if _, err := io.Copy(dstFile, compressedReader); err != nil {
		return err
	}

	// Sync to ensure data is written to disk before returning
	return dstFile.Sync()
}

// compressFileInPlace compresses a file in place
func compressFileInPlace(path string) error {
	// Read original file
	srcFile, err := os.Open(path)
	if err != nil {
		return err
	}

	// Create temporary file for compressed content
	tempPath := path + ".tmp"
	tempFile, err := os.Create(tempPath)
	if err != nil {
		srcFile.Close()
		return err
	}

	// Compress content
	compressedReader := compressions.Compress(srcFile)
	_, copyErr := io.Copy(tempFile, compressedReader)
	compressedReader.Close()
	tempFile.Close()

	if copyErr != nil {
		os.Remove(tempPath)
		return copyErr
	}

	// Replace original file with compressed version
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		return err
	}

	return nil
}

// createTempDecompressedFile creates a temporary file with decompressed content for file type detection
func createTempDecompressedFile(compressedFilePath string) (string, error) {
	compressedFile, err := os.Open(compressedFilePath)
	if err != nil {
		return "", err
	}
	defer compressedFile.Close()

	decompressedReader := compressions.Decompress(compressedFile)
	defer decompressedReader.Close()

	tempFile, err := os.CreateTemp("", "pogo-decompress-*")
	if err != nil {
		return "", err
	}
	defer tempFile.Close()

	_, err = io.Copy(tempFile, decompressedReader)
	if err != nil {
		os.Remove(tempFile.Name())
		return "", err
	}

	return tempFile.Name(), nil
}

// isZstdCompressed checks if a file is compressed with zstd by checking the magic bytes
func isZstdCompressed(filePath string) bool {
	file, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer file.Close()

	magic := make([]byte, 4)
	n, err := file.Read(magic)
	if err != nil || n < 4 {
		return false
	}

	// zstd magic number: 0x28 0xB5 0x2F 0xFD
	return magic[0] == 0x28 && magic[1] == 0xB5 && magic[2] == 0x2F && magic[3] == 0xFD
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
	if len(hash) < 2 {
		return ""
	}
	return filepath.Join(rootDir, hash[:2], hash[2:])
}

// OpenFileByHashWithMime opens a file by its hash and returns a reader and content type
func OpenFileByHashWithMime(hash string) (io.ReadCloser, string, error) {
	filePath := GetFilePathFromHash(hash)
	file, err := os.Open(filePath)
	if err != nil {
		return nil, "", err
	}

	// Check if file is compressed and decompress if needed
	var reader io.ReadCloser
	if isZstdCompressed(filePath) {
		reader = compressions.Decompress(file)
	} else {
		reader = file
	}

	// Read first 512 bytes to detect content type
	buffer := make([]byte, 512)
	n, err := reader.Read(buffer)
	if err != nil && err != io.EOF {
		reader.Close()
		return nil, "", err
	}

	// We can't seek on the potentially decompressed reader, so we need to create a new one
	reader.Close()

	// Reopen the file and setup reader again for the final reader
	file, err = os.Open(filePath)
	if err != nil {
		return nil, "", err
	}

	if isZstdCompressed(filePath) {
		reader = compressions.Decompress(file)
	} else {
		reader = file
	}

	// Detect content type using http.DetectContentType
	contentType := "application/octet-stream"
	if n > 0 {
		contentType = http.DetectContentType(buffer[:n])
	}

	return reader, contentType, nil
}

// OpenFileByHash opens a file by its hash and returns a reader
func OpenFileByHash(hash string) (io.ReadCloser, error) {
	filePath := GetFilePathFromHash(hash)
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}

	// Conditionally decompress based on file content
	if isZstdCompressed(filePath) {
		return compressions.Decompress(file), nil
	}
	return file, nil
}

// OpenFileByHashWithType opens a file by its hash and returns a reader and content type
func OpenFileByHashWithType(hash string) (io.ReadCloser, FileType, error) {
	filePath := GetFilePathFromHash(hash)

	var t FileType
	var err error

	// For file type detection, check if we need decompression
	if isZstdCompressed(filePath) {
		// For compressed files, we need to create a temporary file with decompressed content
		tempFile, err := createTempDecompressedFile(filePath)
		if err != nil {
			return nil, FileType{}, errors.Join(fmt.Errorf("create temp decompressed file %s", filePath), err)
		}
		defer os.Remove(tempFile)

		t, err = DetectFileType(tempFile)
		if err != nil {
			return nil, t, errors.Join(fmt.Errorf("detect file type %s", filePath), err)
		}
	} else {
		// For uncompressed files, we can detect type directly
		t, err = DetectFileType(filePath)
		if err != nil {
			return nil, t, errors.Join(fmt.Errorf("detect file type %s", filePath), err)
		}
	}

	// Open file for the final reader with conditional decompression
	file, err := os.Open(filePath)
	if err != nil {
		return nil, FileType{}, errors.Join(fmt.Errorf("open file %s", filePath), err)
	}

	if isZstdCompressed(filePath) {
		return compressions.Decompress(file), t, nil
	}
	return file, t, nil
}
