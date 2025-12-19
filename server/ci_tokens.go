package server

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
)

// CITokenInfo contains information about a CI access token.
type CITokenInfo struct {
	RepoID int32
}

var (
	ciTokensMu sync.RWMutex
	ciTokens   = make(map[string]*CITokenInfo)
)

// GenerateCIToken creates a new temporary CI access token for the given repository.
// The token is stored in memory and can be used for authenticated requests during CI runs.
func GenerateCIToken(repoID int32) (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}

	token := base64.URLEncoding.EncodeToString(tokenBytes)

	ciTokensMu.Lock()
	ciTokens[token] = &CITokenInfo{
		RepoID: repoID,
	}
	ciTokensMu.Unlock()

	return token, nil
}

// ValidateCIToken checks if the given token is a valid CI token.
// Returns the token info if valid, nil otherwise.
func ValidateCIToken(token string) *CITokenInfo {
	ciTokensMu.RLock()
	defer ciTokensMu.RUnlock()
	return ciTokens[token]
}

// RevokeCIToken removes the given token from the CI token store.
func RevokeCIToken(token string) {
	ciTokensMu.Lock()
	delete(ciTokens, token)
	ciTokensMu.Unlock()
}

