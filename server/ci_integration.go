package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"path/filepath"

	"github.com/pogo-vcs/pogo/db"
	"github.com/pogo-vcs/pogo/filecontents"
	"github.com/pogo-vcs/pogo/server/ci"
	"github.com/pogo-vcs/pogo/server/env"
)

var ciExecutor = ci.NewExecutor()

func getCIConfigFiles(ctx context.Context, changeId int64) (map[string][]byte, error) {
	files, err := db.Q.GetCIConfigFiles(ctx, changeId)
	if err != nil {
		return nil, fmt.Errorf("get CI config files: %w", err)
	}

	configFiles := make(map[string][]byte)
	for _, file := range files {
		if isCIConfigFile(file.Name) {
			content, err := readFileContent(file.ContentHash)
			if err != nil {
				return nil, fmt.Errorf("read file content for %s: %w", file.Name, err)
			}
			configFiles[file.Name] = content
		}
	}

	return configFiles, nil
}

func isCIConfigFile(filename string) bool {
	ext := filepath.Ext(filename)
	return ext == ".yaml" || ext == ".yml"
}

func readFileContent(contentHash []byte) ([]byte, error) {
	hashStr := base64.URLEncoding.EncodeToString(contentHash)
	reader, err := filecontents.OpenFileByHash(hashStr)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

func executeCIForBookmarkEvent(ctx context.Context, changeId int64, bookmarkName string, eventType ci.EventType) {
	configFiles, err := getCIConfigFiles(ctx, changeId)
	if err != nil || len(configFiles) == 0 {
		// No CI configuration or error reading it - silently continue
		return
	}

	// Get repository info to build archive URL
	change, err := db.Q.GetChange(ctx, changeId)
	if err != nil {
		// Log error but don't fail
		fmt.Printf("CI execution error: failed to get change: %v\n", err)
		return
	}

	repo, err := db.Q.GetRepository(ctx, change.RepositoryID)
	if err != nil {
		// Log error but don't fail
		fmt.Printf("CI execution error: failed to get repository: %v\n", err)
		return
	}

	archiveUrl := fmt.Sprintf("%s/repository/%s/archive/%s", env.PublicAddress, repo.Name, bookmarkName)

	event := ci.Event{
		Rev:        bookmarkName,
		ArchiveUrl: archiveUrl,
	}

	// Execute CI in a goroutine to avoid blocking the bookmark operation
	go func() {
		if err := ciExecutor.ExecuteForBookmarkEvent(context.Background(), configFiles, event, eventType); err != nil {
			// Log error but don't fail the bookmark operation
			fmt.Printf("CI execution error: %v\n", err)
		}
	}()
}
