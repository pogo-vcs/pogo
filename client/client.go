package client

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/pogo-vcs/pogo/protos"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

const keyringServiceName = "com.pogo-vcs.pogo"

type Client struct {
	ctx        context.Context
	Token      []byte
	Grpc       *grpc.ClientConn
	Pogo       protos.PogoClient
	Location   string
	repoStore  *RepoStore
	VerboseOut io.Writer
}

func OpenFromFile(ctx context.Context, location string) (*Client, error) {
	file, err := FindRepoFile(location)
	if err != nil {
		return nil, errors.Join(errors.New("find repo file"), err)
	}

	repoStore, err := OpenRepoStore(filepath.Dir(file))
	if err != nil {
		return nil, errors.Join(errors.New("open repo store"), err)
	}

	server, err := repoStore.GetServer()
	if err != nil {
		repoStore.Close()
		return nil, errors.Join(errors.New("get server from repo store"), err)
	}

	// Get or create token for this server
	token, err := GetOrCreateToken(server)
	if err != nil {
		repoStore.Close()
		return nil, errors.Join(errors.New("get token"), err)
	}

	client := &Client{
		ctx:        ctx,
		Token:      token,
		Location:   filepath.Dir(file),
		repoStore:  repoStore,
		VerboseOut: io.Discard,
	}

	grpcClient, err := createGRPCClientWithTLSDetection(ctx, server, client.VerboseOut)
	if err != nil {
		client.Close()
		return nil, errors.Join(fmt.Errorf("open grpc client targeting %s", server), err)
	}

	client.Grpc = grpcClient
	client.Pogo = protos.NewPogoClient(grpcClient)

	return client, nil
}

func (c *Client) ConfigSetChangeId(changeId int64) {
	if err := c.repoStore.SetChangeId(changeId); err != nil {
		panic(err)
	}
}

func FindRepoFile(root string) (string, error) {
	dir := root
	for range 10 {
		file := filepath.Join(dir, ".pogo.db")
		if _, err := os.Stat(file); err == nil {
			return file, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("not found")
		}
		dir = parent
	}
	return "", errors.New("not found")
}

func OpenNew(ctx context.Context, addr string, location string) (*Client, error) {
	if len(addr) == 0 {
		return nil, errors.New("addr is empty")
	}

	// Get or create token for this server
	token, err := GetOrCreateToken(addr)
	if err != nil {
		return nil, errors.Join(errors.New("get token"), err)
	}

	client := &Client{
		ctx:        ctx,
		Token:      token,
		Location:   location,
		repoStore:  nil, // Will be created after Init call
		VerboseOut: io.Discard,
	}

	grpcClient, err := createGRPCClientWithTLSDetection(ctx, addr, client.VerboseOut)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open grpc client targeting %s", addr), err)
	}

	client.Grpc = grpcClient
	client.Pogo = protos.NewPogoClient(grpcClient)

	return client, nil
}

func (c *Client) Close() {
	if c.Grpc != nil {
		_ = c.Grpc.Close()
		c.Grpc = nil
	}
	if c.repoStore != nil {
		_ = c.repoStore.Close()
		c.repoStore = nil
	}
}

func (c *Client) GetAuth() *protos.Auth {
	return &protos.Auth{
		PersonalAccessToken: c.Token,
	}
}

func (c *Client) getServer() string {
	if c.repoStore == nil {
		return ""
	}
	server, _ := c.repoStore.GetServer()
	return server
}

func (c *Client) getRepoId() int32 {
	if c.repoStore == nil {
		return 0
	}
	repoId, _ := c.repoStore.GetRepoId()
	return repoId
}

func (c *Client) getChangeId() int64 {
	if c.repoStore == nil {
		return 0
	}
	changeId, _ := c.repoStore.GetChangeId()
	return changeId
}

func (c *Client) GetRepoStore() *RepoStore {
	return c.repoStore
}

func (c *Client) SetRepoStore(store *RepoStore) {
	c.repoStore = store
}

// detectTLSSupport attempts to determine if the server supports TLS/HTTPS
// It returns (supportsTLS, error) where supportsTLS is true if TLS is supported,
// false if only HTTP is available, and error is non-nil if the server is unreachable
func detectTLSSupport(ctx context.Context, addr string) (bool, error) {

	// Try to establish a TLS connection first
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // Skip certificate verification for detection
	}

	// Attempt TLS connection
	conn, err := tls.DialWithDialer(&net.Dialer{
		Timeout: 3 * time.Second,
	}, "tcp", addr, tlsConfig)

	if err == nil {
		conn.Close()
		return true, nil
	}

	// If TLS fails, try a plain TCP connection to see if the server is reachable at all
	plainConn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		// Server not reachable at all
		return false, fmt.Errorf("server not reachable: %w", err)
	}
	plainConn.Close()

	// Server is reachable but doesn't support TLS
	return false, nil
}

// createGRPCClientWithTLSDetection creates a gRPC client, trying TLS first, then falling back to insecure
func createGRPCClientWithTLSDetection(ctx context.Context, addr string, verboseOut io.Writer) (*grpc.ClientConn, error) {
	// First, detect if the server supports TLS
	supportsTLS, err := detectTLSSupport(ctx, addr)
	if err != nil {
		return nil, errors.Join(errors.New("detect tls support"), err)
	}

	if supportsTLS {
		fmt.Fprintf(verboseOut, "Connecting to %s using HTTPS/TLS...\n", addr)

		// Try to connect with TLS
		tlsConfig := &tls.Config{
			InsecureSkipVerify: true, // Allow self-signed certificates
		}
		creds := credentials.NewTLS(tlsConfig)

		grpcClient, err := grpc.NewClient(
			addr,
			grpc.WithUserAgent("pogo"),
			grpc.WithTransportCredentials(creds),
		)
		if err == nil {
			fmt.Fprintf(verboseOut, "Successfully connected using HTTPS/TLS\n")
			return grpcClient, nil
		}

		// TLS detection succeeded but gRPC connection failed, fall back to insecure
		fmt.Fprintf(os.Stderr, "Warning: TLS connection failed, falling back to insecure connection: %v\n", err)
	}

	fmt.Fprintf(verboseOut, "Connecting to %s using HTTP (insecure)...\n", addr)

	// Fall back to insecure connection
	grpcClient, err := grpc.NewClient(
		addr,
		grpc.WithUserAgent("pogo"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err == nil {
		fmt.Fprintf(verboseOut, "Successfully connected using HTTP (insecure)\n")
	}

	return grpcClient, err
}
