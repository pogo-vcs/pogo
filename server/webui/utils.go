package webui

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pogo-vcs/pogo/auth"
	"github.com/pogo-vcs/pogo/compressions"
	"github.com/pogo-vcs/pogo/db"
	"github.com/pogo-vcs/pogo/secrets"
)

func GetParam(ctx context.Context, name string) (string, bool) {
	v := ctx.Value(name)
	if v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func GetParamI32(ctx context.Context, name string) (int32, bool) {
	v, ok := GetParam(ctx, name)
	if !ok {
		return 0, false
	}
	i, err := strconv.ParseInt(v, 10, 32)
	if err != nil {
		return 0, false
	}
	return int32(i), true
}

type UiContext struct {
	ctx  context.Context
	req  *http.Request
	user *db.User
}

func NewUiContext(req *http.Request) context.Context {
	ctx := &UiContext{
		ctx: req.Context(),
		req: req,
	}

	if userVal := req.Context().Value(auth.UserCtxKey); userVal != nil {
		if user, ok := userVal.(*db.User); ok {
			ctx.user = user
		}
	}

	return ctx
}

func (c *UiContext) User() *db.User {
	return c.user
}

func (c *UiContext) Deadline() (deadline time.Time, ok bool) {
	return c.ctx.Deadline()
}

func (c *UiContext) Done() <-chan struct{} {
	return c.ctx.Done()
}

func (c *UiContext) Err() error {
	return c.ctx.Err()
}

func (c *UiContext) Value(key any) any {
	switch key := key.(type) {
	case string:
		switch key {
		case auth.UserCtxKey:
			return c.user
		default:
			if v := c.req.PathValue(key); v != "" {
				return v
			}
		}
	}
	return c.ctx.Value(key)
}

func GetUser(ctx context.Context) *db.User {
	up := ctx.Value(auth.UserCtxKey)
	if up == nil {
		return nil
	}
	if user, ok := up.(*db.User); ok {
		return user
	}
	return nil
}

func IsLoggedIn(ctx context.Context) bool {
	up := ctx.Value(auth.UserCtxKey)
	if up == nil {
		return false
	}
	if user, ok := up.(*db.User); ok {
		return user != nil
	}
	return false
}

type FileNode struct {
	Name     string
	FullPath string
	IsDir    bool
	File     *db.GetRepositoryFilesRow
	Children []*FileNode
}

func BuildFileTree(files []db.GetRepositoryFilesRow) *FileNode {
	root := &FileNode{IsDir: true, Children: []*FileNode{}}

	for _, file := range files {
		parts := strings.Split(file.Name, "/")
		current := root

		for i, part := range parts {
			isLastPart := i == len(parts)-1

			var child *FileNode
			for _, c := range current.Children {
				if c.Name == part {
					child = c
					break
				}
			}

			if child == nil {
				child = &FileNode{
					Name:     part,
					FullPath: strings.Join(parts[:i+1], "/"),
					IsDir:    !isLastPart,
					Children: []*FileNode{},
				}
				if isLastPart {
					child.File = &file
				}
				current.Children = append(current.Children, child)
			}

			current = child
		}
	}

	sortFileNodes(root)
	return root
}

func sortFileNodes(node *FileNode) {
	if !node.IsDir {
		return
	}

	sort.Slice(node.Children, func(i, j int) bool {
		if node.Children[i].IsDir != node.Children[j].IsDir {
			return node.Children[i].IsDir
		}
		return node.Children[i].Name < node.Children[j].Name
	})

	for _, child := range node.Children {
		sortFileNodes(child)
	}
}

func GetSanitizedLog(ctx context.Context, repoId int32, compressedLog []byte) string {
	decompressed, err := compressions.DecompressBytes(compressedLog)
	if err != nil {
		return "Error decompressing log: " + err.Error()
	}

	secretRows, err := db.Q.GetAllSecrets(ctx, repoId)
	if err != nil {
		return string(decompressed)
	}

	secretValues := make([]string, 0, len(secretRows))
	for _, secret := range secretRows {
		secretValues = append(secretValues, secret.Value)
	}

	return secrets.Hide(string(decompressed), secretValues)
}

func FormatTimestamptz(ts interface{}) string {
	switch v := ts.(type) {
	case string:
		if v == "" {
			return "N/A"
		}
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return "N/A"
		}
		return t.Format("Jan 2, 2006 3:04 PM")
	default:
		return "N/A"
	}
}

// AssetNode represents a file or directory in the asset tree.
type AssetNode struct {
	Name     string
	FullPath string
	IsDir    bool
	Children []*AssetNode
}

// ListRepoAssets returns a list of all asset paths for a repository.
func ListRepoAssets(repoID int32) []string {
	repoAssetsDir := filepath.Join("data/assets", fmt.Sprintf("%d", repoID))

	var assets []string
	_ = filepath.Walk(repoAssetsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			relPath, _ := filepath.Rel(repoAssetsDir, path)
			assets = append(assets, filepath.ToSlash(relPath))
		}
		return nil
	})

	return assets
}

// BuildAssetTree builds a tree structure from a flat list of asset paths.
func BuildAssetTree(assetPaths []string) *AssetNode {
	root := &AssetNode{IsDir: true, Children: []*AssetNode{}}

	for _, assetPath := range assetPaths {
		parts := strings.Split(assetPath, "/")
		current := root

		for i, part := range parts {
			isLastPart := i == len(parts)-1

			var child *AssetNode
			for _, c := range current.Children {
				if c.Name == part {
					child = c
					break
				}
			}

			if child == nil {
				child = &AssetNode{
					Name:     part,
					FullPath: strings.Join(parts[:i+1], "/"),
					IsDir:    !isLastPart,
					Children: []*AssetNode{},
				}
				current.Children = append(current.Children, child)
			}

			current = child
		}
	}

	sortAssetNodes(root)
	return root
}

func sortAssetNodes(node *AssetNode) {
	if !node.IsDir {
		return
	}

	sort.Slice(node.Children, func(i, j int) bool {
		// Directories first, then alphabetically
		if node.Children[i].IsDir != node.Children[j].IsDir {
			return node.Children[i].IsDir
		}
		return node.Children[i].Name < node.Children[j].Name
	})

	for _, child := range node.Children {
		sortAssetNodes(child)
	}
}
