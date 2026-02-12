package server

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/pogo-vcs/pogo/filecontents"
)

// handleObjectUpload handles PUT /v1/objects/{hash} for uploading file content.
// Files are content-addressed, so no repo-scoping is needed.
func handleObjectUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !IsAuthenticated(r.Context()) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	hash := r.PathValue("hash")
	if hash == "" {
		http.Error(w, "Missing object hash", http.StatusBadRequest)
		return
	}

	// Validate that hash is valid base64url
	hashBytes, err := base64.URLEncoding.DecodeString(hash)
	if err != nil {
		http.Error(w, "Invalid hash encoding", http.StatusBadRequest)
		return
	}
	if len(hashBytes) == 0 {
		http.Error(w, "Empty hash", http.StatusBadRequest)
		return
	}

	// Hold GC read lock to prevent GC from deleting the file mid-write
	// or between upload and PushFull commit
	gcMutex.RLock()
	defer gcMutex.RUnlock()

	// Check if file already exists (idempotent)
	filePath := filecontents.GetFilePathFromHash(hash)
	if _, err := os.Stat(filePath); err == nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Stream request body to temp file
	tmpFile, err := os.CreateTemp("", "pogo-upload-*")
	if err != nil {
		http.Error(w, "Failed to create temp file", http.StatusInternalServerError)
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmpFile, r.Body); err != nil {
		tmpFile.Close()
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}
	tmpFile.Close()

	// Hash the temp file and verify it matches the URL parameter
	computedHash, err := filecontents.HashFile(tmpPath)
	if err != nil {
		http.Error(w, "Failed to hash uploaded file", http.StatusInternalServerError)
		return
	}

	computedHashStr := base64.URLEncoding.EncodeToString(computedHash)
	if computedHashStr != hash {
		http.Error(w, fmt.Sprintf("Hash mismatch: expected %s, got %s", hash, computedHashStr), http.StatusBadRequest)
		return
	}

	// Store the file (handles compression + dedup)
	if _, err := filecontents.StoreFile(tmpPath); err != nil {
		http.Error(w, "Failed to store file", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}
