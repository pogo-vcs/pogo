package server

import (
	"context"
	"errors"
	"fmt"

	auth_ "github.com/pogo-vcs/pogo/auth"
	"github.com/pogo-vcs/pogo/db"
	"github.com/pogo-vcs/pogo/protos"
)

func getUserFromAuth(ctx context.Context, auth *protos.Auth) (*db.User, error) {
	if auth == nil {
		return nil, errors.New("no authentication provided")
	}

	if len(auth.PersonalAccessToken) == 0 {
		return nil, errors.New("no personal access token provided")
	}

	user, err := db.Q.GetUserByToken(ctx, auth.PersonalAccessToken)
	if err != nil {
		return nil, fmt.Errorf("invalid or unknown personal access token")
	}

	return &user, nil
}

func getUserIdFromAuth(ctx context.Context, auth *protos.Auth) (*int32, error) {
	user, err := getUserFromAuth(ctx, auth)
	if err != nil {
		return nil, err
	}
	return &user.ID, nil
}

// checkRepositoryAccessFromAuth validates auth and checks repository access.
// Returns the user ID if access is granted, or an error if not.
func checkRepositoryAccessFromAuth(ctx context.Context, auth *protos.Auth, repositoryID int32) (*int32, error) {
	user, err := getUserFromAuth(ctx, auth)
	if err != nil {
		return nil, fmt.Errorf("authenticate user: %w", err)
	}

	// Check repository access
	repo, err := db.Q.GetRepository(ctx, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("get repository: %w", err)
	}

	// Public repositories are accessible to everyone
	if repo.Public {
		return &user.ID, nil
	}

	// Private repositories require explicit access
	hasAccess, err := db.Q.CheckUserRepositoryAccess(ctx, repositoryID, user.ID)
	if err != nil {
		return nil, fmt.Errorf("check user repository access: %w", err)
	}

	if !hasAccess {
		return nil, errors.Join(auth_.ErrAccessDenied, fmt.Errorf("user %s does not have access to repository %d", user.Username, repositoryID))
	}

	return &user.ID, nil
}