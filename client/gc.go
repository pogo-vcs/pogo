package client

import (
	"context"
	"fmt"

	"github.com/pogo-vcs/pogo/protos"
)

// GarbageCollect triggers garbage collection on the server
func (c *Client) GarbageCollect(ctx context.Context) (*protos.GarbageCollectResponse, error) {
	// Get auth token
	auth := c.GetAuth()

	// Create request
	req := &protos.GarbageCollectRequest{
		Auth: auth,
	}

	// Call server
	resp, err := c.Pogo.GarbageCollect(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("garbage collect: %w", err)
	}

	return resp, nil
}
