package db

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/pogo-vcs/pogo/server/env"
)

func Setup(ctx context.Context) error {
	count, err := Q.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("failed to count users: %w", err)
	}

	// If there are any users, setup is complete
	if count > 0 {
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
