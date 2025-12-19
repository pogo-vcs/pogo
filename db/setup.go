package db

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/pogo-vcs/pogo/server/env"
)

func Setup(ctx context.Context) error {
	// Ensure CI user exists (used for CI token authentication)
	if err := Q.EnsureCIUser(ctx); err != nil {
		return fmt.Errorf("failed to ensure CI user: %w", err)
	}

	count, err := Q.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("failed to count users: %w", err)
	}

	// Count includes CI user (id=-1), so check for > 1
	if count > 1 {
		return nil
	}

	var tokenBytes []byte
	if len(env.RootToken) > 0 {
		if tokenBytes, err = DecodeToken(env.RootToken); err != nil {
			return fmt.Errorf("failed to decode root token: %w", err)
		}
	} else {
		tokenBytes = make([]byte, 32)
		if _, err := rand.Read(tokenBytes); err != nil {
			return fmt.Errorf("failed to generate token: %w", err)
		}
	}

	err = Q.CreateUserWithToken(ctx, "root", tokenBytes)
	if err != nil {
		return fmt.Errorf("failed to create root user with token: %w", err)
	}

	tokenString := EncodeToken(tokenBytes)
	fmt.Printf("Root user created with personal access token: %s\n", tokenString)

	return nil
}
