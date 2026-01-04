package server

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/goproxy/goproxy"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pogo-vcs/pogo/auth"
	"github.com/pogo-vcs/pogo/brand"
	"github.com/pogo-vcs/pogo/db"
	"github.com/pogo-vcs/pogo/filecontents"
	"github.com/pogo-vcs/pogo/server/ci"
	"github.com/pogo-vcs/pogo/server/public"
	"github.com/pogo-vcs/pogo/server/webui"
)

func getTokenFromHeader(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	// Support both "Bearer <token>" and just "<token>" formats
	if after, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
		return after
	}
	return authHeader
}

func getTokenFromQuery(r *http.Request) string {
	return r.URL.Query().Get("token")
}

func getTokenFromCookie(r *http.Request) string {
	cookie, err := r.Cookie("token")
	if err != nil || cookie.Value == "" {
		return ""
	}
	return cookie.Value
}

// CITokenCtxKey is the context key for CI token info
type ciTokenCtxKey string

const CITokenCtxKey = ciTokenCtxKey("ci_token")

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Try to get token with priority: Header > Query > Cookie
		var token string
		if token = getTokenFromHeader(r); token != "" {
			// Token found in header
		} else if token = getTokenFromQuery(r); token != "" {
			// Token found in query parameter
		} else {
			token = getTokenFromCookie(r)
			// Token found in cookie or empty string
		}

		if token != "" {
			// First, try to validate as a regular user token
			tokenBytes, err := auth.Decode(token)
			if err == nil {
				user, err := auth.ValidateToken(r.Context(), tokenBytes)
				if err == nil {
					webUser := &db.User{
						ID:       user.ID,
						Username: user.Username,
					}
					ctx := context.WithValue(r.Context(), auth.UserCtxKey, webUser)
					r = r.WithContext(ctx)
					next(w, r)
					return
				}
			}

			// If regular token validation failed, try CI token
			// CI tokens are stored directly in context without a fake user
			if ciTokenInfo := ValidateCIToken(token); ciTokenInfo != nil {
				ctx := context.WithValue(r.Context(), CITokenCtxKey, ciTokenInfo)
				r = r.WithContext(ctx)
			}
		}
		next(w, r)
	}
}

// GetCITokenInfo returns the CI token info from context if present, nil otherwise.
func GetCITokenInfo(ctx context.Context) *CITokenInfo {
	if info, ok := ctx.Value(CITokenCtxKey).(*CITokenInfo); ok {
		return info
	}
	return nil
}

// CheckRepoAccess checks if the request has access to the given repository.
// It handles both regular user authentication and CI token authentication.
// Returns true if access is granted, false otherwise.
func CheckRepoAccess(ctx context.Context, repoID int32) bool {
	// First check for CI token - CI tokens are scoped to a specific repository
	if ciToken := GetCITokenInfo(ctx); ciToken != nil {
		return ciToken.RepoID == repoID
	}

	// Check for regular user authentication
	userInterface := ctx.Value(auth.UserCtxKey)
	if userInterface == nil {
		return false
	}
	user, ok := userInterface.(*db.User)
	if !ok || user == nil {
		return false
	}

	hasAccess, err := db.Q.CheckUserRepositoryAccess(ctx, repoID, user.ID)
	return err == nil && hasAccess
}

// IsAuthenticated checks if there's any form of authentication (user or CI token).
func IsAuthenticated(ctx context.Context) bool {
	// Check for CI token
	if GetCITokenInfo(ctx) != nil {
		return true
	}
	// Check for user
	if userInterface := ctx.Value(auth.UserCtxKey); userInterface != nil {
		if user, ok := userInterface.(*db.User); ok && user != nil {
			return true
		}
	}
	return false
}

func RegisterWebUI(s *Server) {
	s.httpMux.HandleFunc("/", authMiddleware(rootHandler(webui.Repositories())))
	s.httpMux.HandleFunc("/favicon.svg", brand.LogoHandler)
	s.httpMux.HandleFunc("/public/{file}", public.Handle)
	s.httpMux.HandleFunc("/schemas/ci/{schema}", handleCISchemas)
	s.httpMux.HandleFunc("/repository/{id}", authMiddleware(templComponentToHandler(webui.Repository())))
	s.httpMux.HandleFunc("/repository/{id}/settings", authMiddleware(templComponentToHandler(webui.Settings())))
	s.httpMux.HandleFunc("/repository/{id}/ci", authMiddleware(templComponentToHandler(webui.CIRuns())))
	s.httpMux.HandleFunc("/repository/{id}/ci/{runId}", authMiddleware(templComponentToHandler(webui.CIRunDetail())))
	s.httpMux.HandleFunc("/repository/{repo}/archive/{rev}", authMiddleware(handleZipDownload))
	s.httpMux.HandleFunc("/objects/{hash}/", handleObjectServe)
	s.httpMux.HandleFunc("/assets/", authMiddleware(handleAssets))

	// Auth routes
	s.httpMux.HandleFunc("/login", authMiddleware(templComponentToHandler(webui.Login())))
	s.httpMux.HandleFunc("/api/login", handleLogin)
	s.httpMux.HandleFunc("/api/logout", handleLogout)
	s.httpMux.HandleFunc("/register", handleRegisterPage)
	s.httpMux.HandleFunc("/api/register", handleRegister)
	s.httpMux.HandleFunc("/invites", authMiddleware(templComponentToHandler(webui.Invites())))
	s.httpMux.HandleFunc("/api/invites/create", authMiddleware(handleCreateInvite))
	s.httpMux.HandleFunc("/api/invites/revoke", authMiddleware(handleRevokeInvite))

	// Repository management API routes
	s.httpMux.HandleFunc("/api/repository/{id}/rename", authMiddleware(handleRenameRepository))
	s.httpMux.HandleFunc("/api/repository/{id}/grant", authMiddleware(handleGrantAccess))
	s.httpMux.HandleFunc("/api/repository/{id}/revoke", authMiddleware(handleRevokeAccess))
	s.httpMux.HandleFunc("/api/repository/{id}/visibility", authMiddleware(handleSetRepositoryVisibility))
	s.httpMux.HandleFunc("/api/repository/{id}/secrets/set", authMiddleware(handleSetSecret))
	s.httpMux.HandleFunc("/api/repository/{id}/secrets/delete", authMiddleware(handleDeleteSecret))
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
	// Get the hash from path parameters
	hash := r.PathValue("hash")
	if hash == "" {
		http.Error(w, "Missing object hash", http.StatusBadRequest)
		return
	}

	// Determine the requested file path (if any) from the URL.
	rawPath := r.URL.Path
	prefix := "/objects/" + hash
	filename := ""
	if strings.HasPrefix(rawPath, prefix) {
		filename = strings.TrimPrefix(rawPath[len(prefix):], "/")
	}

	displayName := hash
	if filename != "" {
		displayName = "/" + path.Clean(filename)
		if !strings.HasPrefix(displayName, "/") {
			displayName = "/" + displayName
		}
	}

	if isBrowserRequest(r) {
		highlighted, ok, err := webui.HighlightedObjectComponent(r.Context(), filename, hash)
		if err != nil {
			log.Printf("highlight object %s/%s: %v", hash, filename, err)
		} else if ok {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "public, max-age=31536000")
			component := webui.SyntaxHighlightedLayout(displayName, highlighted)
			if err := component.Render(r.Context(), w); err != nil {
				http.Error(w, "Failed to render highlighted view", http.StatusInternalServerError)
			}
			return
		}
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

func isBrowserRequest(r *http.Request) bool {
	// Only allow safe methods that browsers use to render documents.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}

	accept := strings.ToLower(r.Header.Get("Accept"))
	if accept == "" || !strings.Contains(accept, "text/html") {
		return false
	}

	ua := strings.ToLower(r.Header.Get("User-Agent"))
	if ua == "" {
		return false
	}

	knownBrowserTokens := []string{
		"mozilla/",
		"chrome/",
		"safari/",
		"firefox/",
		"edg/",
		"opera/",
	}

	isBrowserUA := false
	for _, token := range knownBrowserTokens {
		if strings.Contains(ua, token) {
			isBrowserUA = true
			break
		}
	}
	if !isBrowserUA {
		return false
	}

	// Respect Fetch metadata headers when available.
	if dest := strings.ToLower(r.Header.Get("Sec-Fetch-Dest")); dest != "" && dest != "document" && dest != "empty" {
		return false
	}
	if mode := strings.ToLower(r.Header.Get("Sec-Fetch-Mode")); mode != "" && mode != "navigate" && mode != "same-origin" {
		return false
	}

	return true
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
	rev := r.PathValue("rev")

	// Check if user has repository access for private repos
	repository, err := db.Q.GetRepositoryByName(ctx, repo)
	if err != nil {
		http.Error(w, "Repository not found", http.StatusNotFound)
		return
	}

	if !repository.Public {
		if !CheckRepoAccess(ctx, repository.ID) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Get files for the change using the revision-fuzzy method
	vcsFiles, err := db.Q.GetRepositoryFilesForRevisionFuzzy(ctx, repository.ID, rev)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get files for revision %q: %s", rev, err.Error()), http.StatusInternalServerError)
		return
	}

	// Set headers before writing any data
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.zip", repo, rev))

	// Create zip writer directly on the response writer
	zipWriter := zip.NewWriter(w)
	defer zipWriter.Close()

	for _, vcsFile := range vcsFiles {
		// Check if file is a symlink
		if vcsFile.SymlinkTarget != nil {
			// For symlinks, create a text file with the target path
			// ZIP format doesn't have great cross-platform symlink support
			zipEntryName := vcsFile.Name + ".symlink"
			fileWriter, err := zipWriter.Create(zipEntryName)
			if err != nil {
				log.Printf("Failed to create zip entry %q: %s", zipEntryName, err.Error())
				return
			}

			symlinkInfo := fmt.Sprintf("SYMLINK: %s\nTarget: %s\n", vcsFile.Name, *vcsFile.SymlinkTarget)
			if _, err := fileWriter.Write([]byte(symlinkInfo)); err != nil {
				log.Printf("Failed to write symlink info %q to zip: %s", vcsFile.Name, err.Error())
				return
			}
		} else {
			// Regular file
			hashStr := base64.URLEncoding.EncodeToString(vcsFile.ContentHash)
			f, err := filecontents.OpenFileByHash(hashStr)
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

func handleCISchemas(w http.ResponseWriter, r *http.Request) {
	// Extract the schema filename from the URL path
	schemaFile := r.PathValue("schema")
	if schemaFile == "" {
		http.Error(w, "Schema file not specified", http.StatusBadRequest)
		return
	}

	// Try to open the file from the CI embedded filesystem
	f, err := ci.Schemas.Open(schemaFile)
	if err != nil {
		http.Error(w, "Schema not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	// Set appropriate content type based on file extension
	var contentType string
	switch path.Ext(schemaFile) {
	case ".json":
		contentType = "application/json"
	case ".xsd":
		contentType = "application/xml"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=3600") // Cache for 1 hour
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Copy the file content to the response
	_, _ = io.Copy(w, f)
}

func handleRegisterPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	inviteToken := r.URL.Query().Get("invite")
	if inviteToken == "" {
		http.Error(w, "Missing invite token", http.StatusBadRequest)
		return
	}

	// Validate invite token
	tokenBytes, err := auth.Decode(inviteToken)
	if err != nil {
		http.Error(w, "Invalid invite token format", http.StatusBadRequest)
		return
	}

	invite, err := db.Q.GetInviteByToken(r.Context(), tokenBytes)
	if err != nil {
		http.Error(w, "Invalid or expired invite token", http.StatusBadRequest)
		return
	}

	// Check if invite is still valid
	if invite.UsedAt.Valid {
		http.Error(w, "This invite has already been used", http.StatusBadRequest)
		return
	}

	if time.Now().After(invite.ExpiresAt.Time) {
		http.Error(w, "This invite has expired", http.StatusBadRequest)
		return
	}

	// Render registration page
	component := webui.Register(inviteToken)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
	}
}

// validUsernamePattern defines the allowed characters for usernames
var validUsernamePattern = regexp.MustCompile("^[a-zA-Z0-9_-]+$")

// validateUsername checks if a username meets the required format
func validateUsername(username string) error {
	if len(username) == 0 {
		return errors.New("Username is required")
	}
	if len(username) < 3 {
		return errors.New("Username must be at least 3 characters long")
	}
	if len(username) > 32 {
		return errors.New("Username must be no more than 32 characters long")
	}
	if !validUsernamePattern.MatchString(username) {
		return errors.New("Username can only contain letters, numbers, underscores, and hyphens")
	}
	return nil
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	inviteToken := r.FormValue("invite_token")

	// Validate username format
	if err := validateUsername(username); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if inviteToken == "" {
		http.Error(w, "Invite token is required", http.StatusBadRequest)
		return
	}

	// Validate invite token
	tokenBytes, err := auth.Decode(inviteToken)
	if err != nil {
		http.Error(w, "Invalid invite token format", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	tx, err := db.Q.Begin(ctx)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Close()

	invite, err := tx.GetInviteByToken(ctx, tokenBytes)
	if err != nil {
		http.Error(w, "Invalid or expired invite token", http.StatusBadRequest)
		return
	}

	// Check if invite is still valid
	if invite.UsedAt.Valid {
		http.Error(w, "This invite has already been used", http.StatusBadRequest)
		return
	}

	if time.Now().After(invite.ExpiresAt.Time) {
		http.Error(w, "This invite has expired", http.StatusBadRequest)
		return
	}

	// Check if username already exists
	if _, err := tx.GetUserByUsername(ctx, username); err == nil {
		http.Error(w, "Username already exists", http.StatusConflict)
		return
	}

	// Generate secure personal access token for new user
	userToken, err := generateSecureToken()
	if err != nil {
		http.Error(w, "Failed to generate user token", http.StatusInternalServerError)
		return
	}

	// Create user with token
	err = tx.CreateUserWithToken(ctx, username, userToken)
	if err != nil {
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	// Get the created user to get their ID
	newUser, err := tx.GetUserByUsername(ctx, username)
	if err != nil {
		http.Error(w, "Failed to retrieve new user", http.StatusInternalServerError)
		return
	}

	// Mark invite as used - this is the final step before commit
	err = tx.UseInvite(ctx, tokenBytes, &newUser.ID)
	if err != nil {
		http.Error(w, "Failed to mark invite as used", http.StatusInternalServerError)
		return
	}

	// Commit the transaction - only after this point is the invite truly "used"
	if err = tx.Commit(ctx); err != nil {
		http.Error(w, "Failed to complete registration", http.StatusInternalServerError)
		return
	}

	// Set authentication cookie for automatic login
	userTokenStr := auth.Encode(userToken)
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    userTokenStr,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil, // Set Secure flag if using HTTPS
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 60 * 60, // 30 days
	})

	// Return success response with token
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]any{
		"success":  true,
		"username": username,
		"token":    userTokenStr,
	}
	json.NewEncoder(w).Encode(response)
}

func handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get authenticated user from context
	user := webui.GetUser(r.Context())
	if user == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	hoursStr := r.FormValue("hours")
	if hoursStr == "" {
		http.Error(w, "Hours parameter is required", http.StatusBadRequest)
		return
	}

	hours, err := strconv.ParseInt(hoursStr, 10, 64)
	if err != nil || hours <= 0 {
		http.Error(w, "Invalid hours value", http.StatusBadRequest)
		return
	}

	// Generate secure invite token
	inviteToken, err := generateSecureToken()
	if err != nil {
		http.Error(w, "Failed to generate invite token", http.StatusInternalServerError)
		return
	}

	// Calculate expiration time
	expiresAt := pgtype.Timestamptz{
		Time:  time.Now().Add(time.Duration(hours) * time.Hour),
		Valid: true,
	}

	// Create invite in database
	ctx := r.Context()
	_, err = db.Q.CreateInvite(ctx, inviteToken, user.ID, expiresAt)
	if err != nil {
		http.Error(w, "Failed to create invite", http.StatusInternalServerError)
		return
	}

	// Generate invite URL
	inviteTokenStr := auth.Encode(inviteToken)
	inviteURL := fmt.Sprintf("%s/register?invite=%s", getPublicAddress(), inviteTokenStr)

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]any{
		"success":    true,
		"invite_url": inviteURL,
		"token":      inviteTokenStr,
	}
	json.NewEncoder(w).Encode(response)
}

func handleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get authenticated user from context
	user := webui.GetUser(r.Context())
	if user == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	inviteToken := r.FormValue("token")
	if inviteToken == "" {
		http.Error(w, "Token parameter is required", http.StatusBadRequest)
		return
	}

	// Decode the invite token
	tokenBytes, err := auth.Decode(inviteToken)
	if err != nil {
		http.Error(w, "Invalid token format", http.StatusBadRequest)
		return
	}

	// Revoke the invite (only if it belongs to the user and is unused)
	ctx := r.Context()
	err = db.Q.RevokeInvite(ctx, tokenBytes, user.ID)
	if err != nil {
		http.Error(w, "Failed to revoke invite or invite not found", http.StatusInternalServerError)
		return
	}

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]any{
		"success": true,
	}
	json.NewEncoder(w).Encode(response)
}

func handleSetRepositoryVisibility(w http.ResponseWriter, r *http.Request) {
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

	hasAccess, err := db.Q.CheckUserRepositoryAccess(ctx, int32(repoId), user.ID)
	if err != nil || !hasAccess {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}
	publicStr := r.FormValue("public")
	if publicStr == "" {
		http.Error(w, "Public value is required", http.StatusBadRequest)
		return
	}

	public := publicStr == "true"

	if err := db.Q.UpdateRepositoryVisibility(ctx, int32(repoId), public); err != nil {
		http.Error(w, fmt.Sprintf("Failed to update repository visibility: %v", err), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/repository/%d/settings", repoId), http.StatusSeeOther)
}

func handleSetSecret(w http.ResponseWriter, r *http.Request) {
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

	hasAccess, err := db.Q.CheckUserRepositoryAccess(ctx, int32(repoId), user.ID)
	if err != nil || !hasAccess {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}
	key := r.FormValue("key")
	value := r.FormValue("value")
	if key == "" || value == "" {
		http.Error(w, "Key and value are required", http.StatusBadRequest)
		return
	}

	if err := db.Q.SetSecret(ctx, int32(repoId), key, value); err != nil {
		http.Error(w, fmt.Sprintf("Failed to set secret: %v", err), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/repository/%d/settings", repoId), http.StatusSeeOther)
}

func handleDeleteSecret(w http.ResponseWriter, r *http.Request) {
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

	hasAccess, err := db.Q.CheckUserRepositoryAccess(ctx, int32(repoId), user.ID)
	if err != nil || !hasAccess {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}
	key := r.FormValue("key")
	if key == "" {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	if err := db.Q.DeleteSecret(ctx, int32(repoId), key); err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete secret: %v", err), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/repository/%d/settings", repoId), http.StatusSeeOther)
}
