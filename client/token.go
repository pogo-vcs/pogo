package client

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/pogo-vcs/pogo/auth"
	"github.com/pogo-vcs/pogo/tty"
)

func getKeyringKey(server string) string {
	return "dev.frankmayer.pogo::" + server
}

func GetToken(server string) ([]byte, error) {
	key := getKeyringKey(server)
	tokenStr, err := keyringGet("pogo", key)
	if err != nil {
		return nil, fmt.Errorf("get token from keyring: %w", err)
	}
	token, err := auth.Decode(tokenStr)
	if err != nil {
		return nil, fmt.Errorf("decode token: %w", err)
	}
	return token, nil
}

func SetToken(server string, token []byte) error {
	key := getKeyringKey(server)
	tokenStr := auth.Encode(token)
	if err := keyringSet("pogo", key, tokenStr); err != nil {
		return fmt.Errorf("set token in keyring: %w", err)
	}
	return nil
}

func RemoveToken(server string) error {
	key := getKeyringKey(server)
	if err := keyringDelete("pogo", key); err != nil {
		return fmt.Errorf("remove token from keyring: %w", err)
	}
	return nil
}

func GetOrCreateToken(server string) ([]byte, error) {
	// Try to get existing token
	token, err := GetToken(server)
	if err == nil {
		return token, nil
	}

	if !tty.IsInteractive() {
		return nil, fmt.Errorf("no token found and not running in interactive mode")
	}

	// No token exists, ask user to provide one
	var tokenStr string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Personal Access Token").
				Description("Please enter your personal access token for " + server).
				Placeholder("Enter your token here").
				Value(&tokenStr),
		),
	)

	if err := form.Run(); err != nil {
		return nil, fmt.Errorf("run form: %w", err)
	}

	if tokenStr == "" {
		return nil, fmt.Errorf("no personal access token provided")
	}

	// Try to decode as base64 first
	token, err = auth.Decode(tokenStr)
	if err != nil {
		return nil, fmt.Errorf("decode token: %w", err)
	}

	// Store in keyring
	if err := SetToken(server, token); err != nil {
		return nil, err
	}

	return token, nil
}
