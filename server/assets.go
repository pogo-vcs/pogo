package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pogo-vcs/pogo/auth"
	"github.com/pogo-vcs/pogo/db"
)

const assetsDir = "data/assets"

// ListAssetsForRepo returns a list of all asset paths for a repository.
// This is used by both the HTTP handler and the web UI.
func ListAssetsForRepo(repoID int32) ([]string, error) {
	repoAssetsDir := filepath.Join(assetsDir, fmt.Sprintf("%d", repoID))

	var assets []string
	err := filepath.Walk(repoAssetsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			relPath, _ := filepath.Rel(repoAssetsDir, path)
			assets = append(assets, filepath.ToSlash(relPath))
		}
		return nil
	})

	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	if assets == nil {
		assets = []string{}
	}

	return assets, nil
}

// handleAssets handles all asset-related HTTP requests.
// Routes:
//   - PUT /assets/{repo_id}/{asset_name...} - Upload asset (requires auth)
//   - GET /assets/{repo_id}/{asset_name...} - Download asset (public)
//   - GET /assets/{repo_id}/ - List assets for repo (public)
//   - DELETE /assets/{repo_id}/{asset_name...} - Delete asset (requires auth)
func handleAssets(w http.ResponseWriter, r *http.Request) {
	// Parse the path: /assets/{repo_id}/{asset_name...}
	path := strings.TrimPrefix(r.URL.Path, "/assets/")
	if path == "" {
		http.Error(w, "Missing repository ID", http.StatusBadRequest)
		return
	}

	// Split into repo_id and asset_name
	parts := strings.SplitN(path, "/", 2)
	repoIDStr := parts[0]

	repoID, err := strconv.ParseInt(repoIDStr, 10, 32)
	if err != nil {
		http.Error(w, "Invalid repository ID", http.StatusBadRequest)
		return
	}

	assetName := ""
	if len(parts) > 1 {
		assetName = parts[1]
	}

	switch r.Method {
	case http.MethodGet:
		if assetName == "" {
			handleListAssets(w, r, int32(repoID))
		} else {
			handleGetAsset(w, r, int32(repoID), assetName)
		}
	case http.MethodPut:
		handlePutAsset(w, r, int32(repoID), assetName)
	case http.MethodDelete:
		handleDeleteAsset(w, r, int32(repoID), assetName)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleListAssets returns a JSON list of assets for a repository.
// This is public - no authentication required.
func handleListAssets(w http.ResponseWriter, r *http.Request, repoID int32) {
	assets, err := ListAssetsForRepo(repoID)
	if err != nil {
		http.Error(w, "Failed to list assets", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(assets)
}

// handleGetAsset serves an asset file.
// This is public - no authentication required.
func handleGetAsset(w http.ResponseWriter, r *http.Request, repoID int32, assetName string) {
	assetPath := filepath.Join(assetsDir, fmt.Sprintf("%d", repoID), filepath.FromSlash(assetName))

	// Security: ensure the resolved path is within the assets directory
	absAssetsDir, _ := filepath.Abs(assetsDir)
	absAssetPath, _ := filepath.Abs(assetPath)
	if !strings.HasPrefix(absAssetPath, absAssetsDir) {
		http.Error(w, "Invalid asset path", http.StatusBadRequest)
		return
	}

	file, err := os.Open(assetPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Asset not found", http.StatusNotFound)
		} else {
			http.Error(w, "Failed to open asset", http.StatusInternalServerError)
		}
		return
	}
	defer file.Close()

	// Get file info for Content-Length
	stat, err := file.Stat()
	if err != nil {
		http.Error(w, "Failed to stat asset", http.StatusInternalServerError)
		return
	}

	// Check if it's a directory
	if stat.IsDir() {
		http.Error(w, "Asset not found", http.StatusNotFound)
		return
	}

	// Set content disposition with the filename
	filename := filepath.Base(assetName)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))

	// Detect content type from first 512 bytes
	buffer := make([]byte, 512)
	n, _ := file.Read(buffer)
	contentType := http.DetectContentType(buffer[:n])
	w.Header().Set("Content-Type", contentType)

	// Seek back to beginning
	file.Seek(0, 0)

	// Stream the file
	io.Copy(w, file)
}

// handlePutAsset uploads an asset file.
// Requires authentication with repository access.
func handlePutAsset(w http.ResponseWriter, r *http.Request, repoID int32, assetName string) {
	if assetName == "" {
		http.Error(w, "Asset name is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Check authentication
	userInterface := ctx.Value(auth.UserCtxKey)
	if userInterface == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	user, ok := userInterface.(*db.User)
	if !ok || user == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Check if this is a CI token - if so, verify repo access
	if ciTokenInfo := ctx.Value(CITokenCtxKey); ciTokenInfo != nil {
		tokenInfo := ciTokenInfo.(*CITokenInfo)
		if tokenInfo.RepoID != repoID {
			http.Error(w, "CI token does not have access to this repository", http.StatusForbidden)
			return
		}
	} else {
		// Regular user - check repository access
		hasAccess, err := db.Q.CheckUserRepositoryAccess(ctx, repoID, user.ID)
		if err != nil || !hasAccess {
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}
	}

	assetPath := filepath.Join(assetsDir, fmt.Sprintf("%d", repoID), filepath.FromSlash(assetName))

	// Security: ensure the resolved path is within the assets directory
	absAssetsDir, _ := filepath.Abs(assetsDir)
	absAssetPath, _ := filepath.Abs(assetPath)
	if !strings.HasPrefix(absAssetPath, absAssetsDir) {
		http.Error(w, "Invalid asset path", http.StatusBadRequest)
		return
	}

	// Create directory structure
	if err := os.MkdirAll(filepath.Dir(assetPath), 0755); err != nil {
		http.Error(w, "Failed to create directory", http.StatusInternalServerError)
		return
	}

	// Create the file
	file, err := os.Create(assetPath)
	if err != nil {
		http.Error(w, "Failed to create asset", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Write the request body to the file
	written, err := io.Copy(file, r.Body)
	if err != nil {
		os.Remove(assetPath)
		http.Error(w, "Failed to write asset", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"size":    written,
		"path":    assetName,
	})
}

// handleDeleteAsset deletes an asset file.
// Requires authentication with repository access.
func handleDeleteAsset(w http.ResponseWriter, r *http.Request, repoID int32, assetName string) {
	if assetName == "" {
		http.Error(w, "Asset name is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Check authentication
	userInterface := ctx.Value(auth.UserCtxKey)
	if userInterface == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	user, ok := userInterface.(*db.User)
	if !ok || user == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Check if this is a CI token - if so, verify repo access
	if ciTokenInfo := ctx.Value(CITokenCtxKey); ciTokenInfo != nil {
		tokenInfo := ciTokenInfo.(*CITokenInfo)
		if tokenInfo.RepoID != repoID {
			http.Error(w, "CI token does not have access to this repository", http.StatusForbidden)
			return
		}
	} else {
		// Regular user - check repository access
		hasAccess, err := db.Q.CheckUserRepositoryAccess(ctx, repoID, user.ID)
		if err != nil || !hasAccess {
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}
	}

	assetPath := filepath.Join(assetsDir, fmt.Sprintf("%d", repoID), filepath.FromSlash(assetName))

	// Security: ensure the resolved path is within the assets directory
	absAssetsDir, _ := filepath.Abs(assetsDir)
	absAssetPath, _ := filepath.Abs(assetPath)
	if !strings.HasPrefix(absAssetPath, absAssetsDir) {
		http.Error(w, "Invalid asset path", http.StatusBadRequest)
		return
	}

	if err := os.Remove(assetPath); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Asset not found", http.StatusNotFound)
		} else {
			http.Error(w, "Failed to delete asset", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

