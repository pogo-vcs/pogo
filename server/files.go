package server

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/pogo-vcs/pogo/db"
	"github.com/pogo-vcs/pogo/filecontents"
)

var defaultIgnorePatterns = []gitignore.Pattern{
	gitignore.ParsePattern(".git", nil),
	gitignore.ParsePattern(".DS_Store", nil),
	gitignore.ParsePattern("Thumbs.db", nil),
	gitignore.ParsePattern(".pogo.yaml", nil),
}

type GetRevisionIgnoreMatcherParams struct {
	revision string
	changeId int64
	repoId   int32
}

func GetRevisionIgnoreMatcher(ctx context.Context, params GetRevisionIgnoreMatcherParams) (gitignore.Matcher, error) {
	var patterns []gitignore.Pattern
	patterns = append(patterns, defaultIgnorePatterns...)

	var ignoreFiles []db.File
	var err error
	if params.changeId > 0 || len(params.revision) == 0 {
		ignoreFiles, err = db.Q.GetRepositoryIgnoreFilesForChangeId(ctx, params.changeId)
	} else {
		ignoreFiles, err = db.Q.GetRepositoryIgnoreFilesForRevisionFuzzy(ctx, params.repoId, params.revision)
	}
	if err != nil {
		return nil, errors.Join(errors.New("get repository ignore files"), err)
	}

	for _, ignoreFile := range ignoreFiles {
		hashStr := base64.URLEncoding.EncodeToString(ignoreFile.ContentHash)
		f, _, err := filecontents.OpenFileByHashWithType(hashStr)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("open ignore file %s", ignoreFile.Name), err)
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			s := strings.TrimSpace(scanner.Text())
			if len(s) == 0 || strings.HasPrefix(s, "#") {
				continue
			}

			relDir := filepath.Dir(ignoreFile.Name)
			if relDir == "." {
				relDir = ""
			}
			var domain []string
			if relDir != "" {
				// Use forward slashes for git paths, regardless of OS
				domain = strings.Split(filepath.ToSlash(relDir), "/")
			}
			patterns = append(patterns, gitignore.ParsePattern(s, domain))
		}

		if err := scanner.Err(); err != nil {
			return nil, errors.Join(fmt.Errorf("scan ignore file %s", ignoreFile.Name), err)
		}
	}

	return gitignore.NewMatcher(patterns), nil
}
