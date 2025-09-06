package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pogo-vcs/pogo/protos"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	ctx      context.Context
	Token    []byte
	Grpc     *grpc.ClientConn
	Pogo     protos.PogoClient
	Location string
	server   string
	repoId   int32
	changeId int64
}

func OpenFromFile(ctx context.Context, location string) (*Client, error) {
	file, err := FindRepoFile(location)
	if err != nil {
		return nil, errors.Join(errors.New("find repo file"), err)
	}
	config := &Repo{}
	if err := config.Load(file); err != nil {
		return nil, errors.Join(errors.New("load repo file"), err)
	}

	// Get or create token for this server
	token, err := GetOrCreateToken(config.Server)
	if err != nil {
		return nil, errors.Join(errors.New("get token"), err)
	}

	grpcClient, err := grpc.NewClient(
		config.Server,
		grpc.WithUserAgent("pogo"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open grpc client targeting %s", config.Server), err)
	}

	pogoClient := protos.NewPogoClient(grpcClient)

	return &Client{
		ctx,
		token,
		grpcClient,
		pogoClient,
		filepath.Dir(file),
		config.Server,
		config.RepoId,
		config.ChangeId,
	}, nil
}

func (c *Client) ConfigSetChangeId(changeId int64) {
	c.changeId = changeId

	repo := &Repo{
		Server:   c.server,
		RepoId:   c.repoId,
		ChangeId: changeId,
	}
	if err := repo.Save(filepath.Join(c.Location, ".pogo.yaml")); err != nil {
		panic(err)
	}
}

func FindRepoFile(root string) (string, error) {
	dir := root
	for range 10 {
		file := filepath.Join(dir, ".pogo.yaml")
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

	grpcClient, err := grpc.NewClient(
		addr,
		grpc.WithUserAgent("pogo"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open grpc client targeting %s", addr), err)
	}

	pogoClient := protos.NewPogoClient(grpcClient)

	return &Client{
		ctx,
		token,
		grpcClient,
		pogoClient,
		location,
		// will be overwritten later:
		addr,
		0,
		0,
	}, nil
}

func (c *Client) Close() {
	if c.Grpc != nil {
		_ = c.Grpc.Close()
		c.Grpc = nil
	}
}

func (c *Client) GetAuth() *protos.Auth {
	return &protos.Auth{
		PersonalAccessToken: c.Token,
	}
}
