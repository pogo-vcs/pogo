package db

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
)

func Setup(ctx context.Context) error {
	count, err := Q.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("failed to count users: %w", err)
	}

	if count > 0 {
		return nil
	}

	var tokenBytes []byte
	if staticRootToken, ok := os.LookupEnv("ROOT_TOKEN"); ok {
		if tokenBytes, err = DecodeToken(staticRootToken); err != nil {
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
