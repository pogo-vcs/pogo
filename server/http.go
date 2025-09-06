package server

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/goproxy/goproxy"
	"github.com/pogo-vcs/pogo/auth"
	"github.com/pogo-vcs/pogo/db"
	"github.com/pogo-vcs/pogo/filecontents"
	"github.com/pogo-vcs/pogo/server/public"
	"github.com/pogo-vcs/pogo/server/webui"
)

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("token")
		if err == nil && cookie.Value != "" {
			tokenBytes, err := auth.Decode(cookie.Value)
			if err == nil {
				user, err := auth.ValidateToken(r.Context(), tokenBytes)
				if err == nil {
					webUser := &db.User{
						ID:       user.ID,
						Username: user.Username,
					}
					ctx := context.WithValue(r.Context(), auth.UserCtxKey, webUser)
					r = r.WithContext(ctx)
				}
			} else {
				fmt.Printf("decode token failed: %v\n", err)
			}
		}
		next(w, r)
	}
}

func RegisterWebUI(s *Server) {
	s.httpMux.HandleFunc("/", authMiddleware(rootHandler(webui.Repositories())))
	s.httpMux.HandleFunc("/public/{file}", public.Handle)
	s.httpMux.HandleFunc("/repository/{id}", authMiddleware(templComponentToHandler(webui.Repository())))
	s.httpMux.HandleFunc("/repository/{id}/settings", authMiddleware(templComponentToHandler(webui.Settings())))
	s.httpMux.HandleFunc("/repository/{repo}/{bookmark}/archive.zip", authMiddleware(handleZipDownload))
	s.httpMux.HandleFunc("/objects/{hash}", handleObjectServe)
	s.httpMux.HandleFunc("/objects/{hash}/{filename}", handleObjectServe)

	// Auth routes
	s.httpMux.HandleFunc("/login", authMiddleware(templComponentToHandler(webui.Login())))
	s.httpMux.HandleFunc("/api/login", handleLogin)
	s.httpMux.HandleFunc("/api/logout", handleLogout)

	// Repository management API routes
	s.httpMux.HandleFunc("/api/repository/{id}/rename", authMiddleware(handleRenameRepository))
	s.httpMux.HandleFunc("/api/repository/{id}/grant", authMiddleware(handleGrantAccess))
	s.httpMux.HandleFunc("/api/repository/{id}/revoke", authMiddleware(handleRevokeAccess))
}

func newGoProxy() *goproxy.Goproxy {
	proxy := &goproxy.Goproxy{
		Fetcher: &goproxyFetcher{},
		Cacher:  &goproxyCacher{},
	}
	return proxy
}

func isGoProxyRequest(r *http.Request) bool {
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 4 {
		return false
	}
	return pathParts[3] == "@v"
}

func rootHandler(index templ.Component) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			index.Render(webui.NewUiContext(r), w)
			return
		}

		http.NotFound(w, r)
	}
}

func templComponentToHandler(c templ.Component) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := webui.NewUiContext(r)
		err := c.Render(ctx, w)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	}
}

func handleObjectServe(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")

	// Get the hash from path parameters
	hash := r.PathValue("hash")
	if hash == "" {
		http.Error(w, "Missing object hash", http.StatusBadRequest)
		return
	}

	// Open the file using the filecontents abstraction
	reader, contentType, err := filecontents.OpenFileByHashWithMime(hash)
	if err != nil {
		http.Error(w, "Object not found", http.StatusNotFound)
		return
	}
	defer reader.Close()

	if filename != "" {
		switch strings.ToLower(path.Ext(filename)) {
		case ".md", ".markdown":
			contentType = "text/markdown"
		case ".org":
			contentType = "text/org"
		case ".go":
			contentType = "text/x-go"
		case ".toml":
			contentType = "text/toml"
		case ".yaml", ".yml":
			contentType = "text/yaml"
		case ".json":
			contentType = "application/json"
		case ".js", ".mjs", ".cjs":
			contentType = "application/javascript"
		case ".css":
			contentType = "text/css"
		case ".html":
			contentType = "text/html"
		}
	}

	// Set headers
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000") // Cache for 1 year

	// Copy the file content to the response
	_, err = io.Copy(w, reader)
	if err != nil {
		http.Error(w, "Failed to serve object", http.StatusInternalServerError)
		return
	}
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := r.FormValue("token")
	if token == "" {
		http.Error(w, "Token is required", http.StatusBadRequest)
		return
	}

	tokenBytes, err := auth.Decode(token)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid token format: %v", err), http.StatusBadRequest)
		return
	}

	_, err = auth.ValidateToken(r.Context(), tokenBytes)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}
		http.Error(w, "Authentication failed", http.StatusInternalServerError)
		return
	}

	// Set httpOnly cookie server-side
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60, // 1 year
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"success": true}`))
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Clear the cookie by setting MaxAge to -1
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"success": true}`))
}

func handleZipDownload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	repo := r.PathValue("repo")
	bookmark := r.PathValue("bookmark")

	vcsFiles, err := db.Q.GetPublicFileHashesByBookmark(ctx, repo, bookmark)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get file hashes for bookmark %q: %s", bookmark, err.Error()), http.StatusInternalServerError)
		return
	}

	// Set headers before writing any data
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.zip", bookmark))

	// Create zip writer directly on the response writer
	zipWriter := zip.NewWriter(w)
	defer zipWriter.Close()

	for _, vcsFile := range vcsFiles {
		filePath := filecontents.GetFilePathFromHash(base64.URLEncoding.EncodeToString(vcsFile.ContentHash))
		f, err := os.Open(filePath)
		if err != nil {
			// Can't use http.Error after headers are sent, log the error instead
			log.Printf("Failed to open file %q for zip: %s", vcsFile.Name, err.Error())
			return
		}

		zipEntryName := vcsFile.Name
		fileWriter, err := zipWriter.Create(zipEntryName)
		if err != nil {
			f.Close()
			log.Printf("Failed to create zip entry %q: %s", zipEntryName, err.Error())
			return
		}

		_, err = io.Copy(fileWriter, f)
		f.Close() // Close immediately after copying
		if err != nil {
			log.Printf("Failed to copy file %q to zip entry: %s", vcsFile.Name, err.Error())
			return
		}
	}
}

func handleRenameRepository(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	userInterface := ctx.Value(auth.UserCtxKey)
	if userInterface == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	user, ok := userInterface.(*db.User)
	if !ok || user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	repoIdStr := r.PathValue("id")
	repoId, err := strconv.ParseInt(repoIdStr, 10, 32)
	if err != nil {
		http.Error(w, "Invalid repository ID", http.StatusBadRequest)
		return
	}

	// Check if user has access to this repository
	hasAccess, err := db.Q.CheckUserRepositoryAccess(ctx, int32(repoId), user.ID)
	if err != nil || !hasAccess {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get new name from form
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}
	newName := r.FormValue("name")
	if newName == "" {
		http.Error(w, "Repository name is required", http.StatusBadRequest)
		return
	}

	// Update repository name
	if err := db.Q.UpdateRepositoryName(ctx, int32(repoId), newName); err != nil {
		http.Error(w, fmt.Sprintf("Failed to rename repository: %v", err), http.StatusInternalServerError)
		return
	}

	// Redirect back to settings page
	http.Redirect(w, r, fmt.Sprintf("/repository/%d/settings", repoId), http.StatusSeeOther)
}

func handleGrantAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	userInterface := ctx.Value(auth.UserCtxKey)
	if userInterface == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	user, ok := userInterface.(*db.User)
	if !ok || user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	repoIdStr := r.PathValue("id")
	repoId, err := strconv.ParseInt(repoIdStr, 10, 32)
	if err != nil {
		http.Error(w, "Invalid repository ID", http.StatusBadRequest)
		return
	}

	// Check if user has access to this repository
	hasAccess, err := db.Q.CheckUserRepositoryAccess(ctx, int32(repoId), user.ID)
	if err != nil || !hasAccess {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get username from form
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	if username == "" {
		http.Error(w, "Username is required", http.StatusBadRequest)
		return
	}

	// Grant access to the user
	if err := db.Q.GrantRepositoryAccessByUsername(ctx, int32(repoId), username); err != nil {
		http.Error(w, fmt.Sprintf("Failed to grant access: %v", err), http.StatusInternalServerError)
		return
	}

	// Redirect back to settings page
	http.Redirect(w, r, fmt.Sprintf("/repository/%d/settings", repoId), http.StatusSeeOther)
}

func handleRevokeAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	userInterface := ctx.Value(auth.UserCtxKey)
	if userInterface == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	user, ok := userInterface.(*db.User)
	if !ok || user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	repoIdStr := r.PathValue("id")
	repoId, err := strconv.ParseInt(repoIdStr, 10, 32)
	if err != nil {
		http.Error(w, "Invalid repository ID", http.StatusBadRequest)
		return
	}

	// Check if user has access to this repository
	hasAccess, err := db.Q.CheckUserRepositoryAccess(ctx, int32(repoId), user.ID)
	if err != nil || !hasAccess {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get username from form
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	if username == "" {
		http.Error(w, "Username is required", http.StatusBadRequest)
		return
	}

	// Don't allow users to revoke their own access
	if username == user.Username {
		http.Error(w, "Cannot revoke your own access", http.StatusBadRequest)
		return
	}

	// Revoke access from the user
	if err := db.Q.RevokeRepositoryAccessByUsername(ctx, int32(repoId), username); err != nil {
		http.Error(w, fmt.Sprintf("Failed to revoke access: %v", err), http.StatusInternalServerError)
		return
	}

	// Redirect back to settings page
	http.Redirect(w, r, fmt.Sprintf("/repository/%d/settings", repoId), http.StatusSeeOther)
}
