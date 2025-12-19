package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pogo-vcs/pogo/compressions"
	"github.com/pogo-vcs/pogo/db"
	"github.com/pogo-vcs/pogo/filecontents"
	"github.com/pogo-vcs/pogo/server/ci"
	"github.com/pogo-vcs/pogo/server/env"
)

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

func extractRepositoryContentToTemp(ctx context.Context, repositoryId int32, bookmarkName string) (string, error) {
	vcsFiles, err := db.Q.GetRepositoryFilesForRevisionFuzzy(ctx, repositoryId, bookmarkName)
	if err != nil {
		return "", fmt.Errorf("get repository files: %w", err)
	}

	tempDir := filepath.Join(os.TempDir(), fmt.Sprintf("pogo-ci-%d-%d", time.Now().UnixNano(), repositoryId))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("create temp directory: %w", err)
	}

	for _, vcsFile := range vcsFiles {
		hashStr := base64.URLEncoding.EncodeToString(vcsFile.ContentHash)
		reader, err := filecontents.OpenFileByHash(hashStr)
		if err != nil {
			os.RemoveAll(tempDir)
			return "", fmt.Errorf("open file %s: %w", vcsFile.Name, err)
		}

		destPath := filepath.Join(tempDir, vcsFile.Name)
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			reader.Close()
			os.RemoveAll(tempDir)
			return "", fmt.Errorf("create directory for %s: %w", vcsFile.Name, err)
		}

		_ = os.MkdirAll(filepath.Dir(destPath), 0755)

		perm := os.FileMode(0644)
		if vcsFile.Executable {
			perm = 0755
		}

		destFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
		if err != nil {
			reader.Close()
			os.RemoveAll(tempDir)
			return "", fmt.Errorf("create file %s: %w", vcsFile.Name, err)
		}

		_, err = io.Copy(destFile, reader)
		reader.Close()
		destFile.Close()
		if err != nil {
			os.RemoveAll(tempDir)
			return "", fmt.Errorf("copy file %s: %w", vcsFile.Name, err)
		}
	}

	return tempDir, nil
}

func storeCIRun(repositoryID int32, res ci.TaskExecutionResult) error {
	start := res.StartedAt
	if start.IsZero() {
		start = time.Now()
	}
	start = start.UTC()

	var pattern *string
	if res.Pattern != "" {
		pattern = &res.Pattern
	}

	startTS := pgtype.Timestamptz{
		Time:  start,
		Valid: true,
	}

	compressedLog, err := compressions.CompressBytes([]byte(res.Log))
	if err != nil {
		return fmt.Errorf("compress log: %w", err)
	}

	var finishTS pgtype.Timestamptz
	if !res.FinishedAt.IsZero() {
		finishTS = pgtype.Timestamptz{
			Time:  res.FinishedAt.UTC(),
			Valid: true,
		}
	}

	_, err = db.Q.CreateCIRun(context.Background(),
		repositoryID,
		res.ConfigFilename,
		res.EventType.String(),
		res.Rev,
		pattern,
		res.Reason,
		res.TaskType,
		int32(res.StatusCode),
		res.Success,
		startTS,
		finishTS,
		compressedLog,
	)
	return err
}

func executeCIForBookmarkEvent(ctx context.Context, changeId int64, bookmarkName string, eventType ci.EventType) {
	configFiles, err := getCIConfigFiles(ctx, changeId)
	if err != nil || len(configFiles) == 0 {
		return
	}

	change, err := db.Q.GetChange(ctx, changeId)
	if err != nil {
		fmt.Printf("CI execution error: failed to get change: %v\n", err)
		return
	}

	repo, err := db.Q.GetRepository(ctx, change.RepositoryID)
	if err != nil {
		fmt.Printf("CI execution error: failed to get repository: %v\n", err)
		return
	}

	var author string
	if change.AuthorID != nil {
		user, err := db.Q.GetUser(ctx, *change.AuthorID)
		if err == nil {
			author = user.Username
		}
	}

	description := ""
	if change.Description != nil {
		description = *change.Description
	}

	archiveUrl := fmt.Sprintf("%s/repository/%s/archive/%s", env.PublicAddress, repo.Name, bookmarkName)

	// Generate a temporary CI access token for this run
	accessToken, err := GenerateCIToken(repo.ID)
	if err != nil {
		fmt.Printf("CI execution error: repo=%s change_id=%d bookmark=%s event=%s detail=generate ci token: %v\n", repo.Name, changeId, bookmarkName, eventType.String(), err)
		return
	}

	event := ci.Event{
		Rev:          bookmarkName,
		ArchiveUrl:   archiveUrl,
		Author:       author,
		Description:  description,
		AccessToken:  accessToken,
		ServerUrl:    env.PublicAddress,
		RepositoryID: repo.ID,
	}

	go func() {
		// Revoke the CI token when done
		defer RevokeCIToken(accessToken)

		tempDir, err := extractRepositoryContentToTemp(context.Background(), change.RepositoryID, bookmarkName)
		if err != nil {
			fmt.Printf("CI execution error: repo=%s change_id=%d bookmark=%s event=%s detail=extract repository content: %v\n", repo.Name, changeId, bookmarkName, eventType.String(), err)
			return
		}
		defer os.RemoveAll(tempDir)

		secrets, err := db.Q.GetAllSecrets(context.Background(), change.RepositoryID)
		if err != nil {
			fmt.Printf("CI execution error: repo=%s change_id=%d bookmark=%s event=%s detail=get secrets: %v\n", repo.Name, changeId, bookmarkName, eventType.String(), err)
			return
		}

		secretsMap := make(map[string]string)
		for _, secret := range secrets {
			secretsMap[secret.Key] = secret.Value
		}

		executor := ci.NewExecutor()
		executor.SetRepoContentDir(tempDir)
		executor.SetSecrets(secretsMap)

		fmt.Printf("CI execution started: repo=%s change_id=%d bookmark=%s event=%s\n", repo.Name, changeId, bookmarkName, eventType.String())

		results, execErr := executor.ExecuteForBookmarkEvent(context.Background(), configFiles, event, eventType)

		for _, res := range results {
			if err := storeCIRun(repo.ID, res); err != nil {
				fmt.Printf("CI execution error: repo=%s change_id=%d bookmark=%s event=%s detail=store ci run: %v\n", repo.Name, changeId, bookmarkName, eventType.String(), err)
			}
		}

		if execErr != nil {
			fmt.Printf("CI execution completed: status=failure repo=%s change_id=%d bookmark=%s event=%s error=%v\n", repo.Name, changeId, bookmarkName, eventType.String(), execErr)
			return
		}

		fmt.Printf("CI execution completed: status=success repo=%s change_id=%d bookmark=%s event=%s runs=%d\n", repo.Name, changeId, bookmarkName, eventType.String(), len(results))
	}()
}
