//go:build fakekeyring

package client

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	fakeKeyringStore = make(map[string]string)
	keyringMutex     sync.RWMutex
)

func keyringGet(service string, user string) (string, error) {
	key := service + "::" + user

	// First check in-memory store
	keyringMutex.RLock()
	value, ok := fakeKeyringStore[key]
	keyringMutex.RUnlock()

	if ok {
		return value, nil
	}

	// Then check file-based fallback for tests that use setupToken()
	// The user parameter is like "dev.frankmayer.pogo::localhost:4321"
	// Extract the server address from it
	parts := strings.SplitN(user, "::", 2)
	server := user
	if len(parts) == 2 {
		server = parts[1]
	}

	tokenDir := filepath.Join(os.Getenv("HOME"), ".config", "pogo", "tokens")
	tokenFile := filepath.Join(tokenDir, server)

	data, err := os.ReadFile(tokenFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("secret not found in keyring")
		}
		return "", err
	}

	return string(data), nil
}

func keyringSet(service string, user string, password string) error {
	key := service + "::" + user

	keyringMutex.Lock()
	fakeKeyringStore[key] = password
	keyringMutex.Unlock()

	// Also write to file for compatibility with existing tests
	// Extract server address from user parameter
	parts := strings.SplitN(user, "::", 2)
	server := user
	if len(parts) == 2 {
		server = parts[1]
	}

	tokenDir := filepath.Join(os.Getenv("HOME"), ".config", "pogo", "tokens")
	if err := os.MkdirAll(tokenDir, 0755); err != nil {
		return err
	}

	tokenFile := filepath.Join(tokenDir, server)
	return os.WriteFile(tokenFile, []byte(password), 0600)
}

func keyringDelete(service string, user string) error {
	key := service + "::" + user

	keyringMutex.Lock()
	delete(fakeKeyringStore, key)
	keyringMutex.Unlock()

	// Also delete file if it exists
	// Extract server address from user parameter
	parts := strings.SplitN(user, "::", 2)
	server := user
	if len(parts) == 2 {
		server = parts[1]
	}

	tokenDir := filepath.Join(os.Getenv("HOME"), ".config", "pogo", "tokens")
	tokenFile := filepath.Join(tokenDir, server)

	err := os.Remove(tokenFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}
