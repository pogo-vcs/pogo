package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/pogo-vcs/pogo/db"
)

var (
	ErrUnauthorized = errors.New("unauthorized: invalid token or user not found")
	ErrAccessDenied = errors.New("access denied")
)

type User struct {
	ID       int32
	Username string
}

type ctxKey = string

const UserCtxKey = ctxKey("pat_user")

func Decode(token string) ([]byte, error) {
	return db.DecodeToken(token)
}

func Encode(token []byte) string {
	return db.EncodeToken(token)
}

func ValidateToken(ctx context.Context, token []byte) (*User, error) {
	if len(token) == 0 {
		return nil, ErrUnauthorized
	}

	dbUser, err := db.Q.GetUserByToken(ctx, token)
	if err != nil {
		return nil, errors.Join(ErrUnauthorized, err)
	}

	return &User{
		ID:       dbUser.ID,
		Username: dbUser.Username,
	}, nil
}

// CheckRepositoryAccess checks if a user has write access to a repository.
// For public repositories, this always returns true.
// For private repositories, it checks if the user has been granted access.
func CheckRepositoryAccess(ctx context.Context, userID *int32, repositoryID int32) (bool, error) {
	// Get repository to check if it's public
	repo, err := db.Q.GetRepository(ctx, repositoryID)
	if err != nil {
		return false, fmt.Errorf("get repository: %w", err)
	}

	// Public repositories are accessible to everyone
	if repo.Public {
		return true, nil
	}

	// Private repositories require explicit access
	if userID == nil {
		return false, nil
	}

	hasAccess, err := db.Q.CheckUserRepositoryAccess(ctx, repositoryID, *userID)
	if err != nil {
		return false, fmt.Errorf("check user repository access: %w", err)
	}

	return hasAccess, nil
}

// CheckRepositoryAccessFromToken validates a token and checks repository access in one step.
// This is a convenience function for gRPC handlers.
func CheckRepositoryAccessFromToken(ctx context.Context, token []byte, repositoryID int32) (*User, error) {
	// First validate the token
	user, err := ValidateToken(ctx, token)
	if err != nil {
		return nil, err
	}

	// Then check repository access
	hasAccess, err := CheckRepositoryAccess(ctx, &user.ID, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("check repository access: %w", err)
	}

	if !hasAccess {
		return nil, errors.Join(ErrAccessDenied, fmt.Errorf("user %s does not have access to repository %d", user.Username, repositoryID))
	}

	return user, nil
}