package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/pogo-vcs/pogo/db"
	"github.com/pogo-vcs/pogo/filecontents"
)

type nopCloser struct{}

func (nopCloser) Close() error               { return nil }
func (nopCloser) Read(p []byte) (int, error) { return 0, io.EOF }

// goproxyFetcher implements goproxy.Fetcher to get data from your VCS.
type goproxyFetcher struct{}

func (f *goproxyFetcher) List(ctx context.Context, path string) ([]string, error) {
	// log.Debug("List", "path", path)
	pathParts := strings.Split(path, "/")
	if strings.HasPrefix(path, "/") {
		pathParts = pathParts[1:]
	}

	switch len(pathParts) {
	case 2:
		repo := pathParts[1]
		bookmarks, err := db.Q.GetPublicRepositoryBookmarks(ctx, repo)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("failed to get bookmarks for repository %q", repo), err)
		}
		return bookmarks, nil
	default:
		return nil, fs.ErrNotExist
	}
}

func (f *goproxyFetcher) Download(ctx context.Context, path, version string) (info, mod, zip io.ReadSeekCloser, err error) {
	// log.Debug("Download", "path", path, "version", version)
	// Your logic to retrieve .info, .mod, and .zip files for 'path' and 'version'.
	// These should be io.ReadSeekCloser implementations.
	return nil, nil, nil, nil // Replace with actual implementation
}

func (f *goproxyFetcher) Query(ctx context.Context, path, query string) (version string, time time.Time, err error) {
	// log.Debug("Query", "path", path, "query", query)
	return
}

// goproxyCacher implements goproxy.Cacher to cache data from your VCS.
type goproxyCacher struct{}

type goproxyCacherGetInfo struct {
	// Version is the bookmark name
	Version string
	// Time is the last modified timestamp of the change the bookmark points to in RFC3339 format
	Time string
}

func (i goproxyCacherGetInfo) Reader() io.ReadCloser {
	b, _ := json.Marshal(i)
	return io.NopCloser(bytes.NewReader(b))
}

func (c *goproxyCacher) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	// log.Debug("Get", "name", name)
	pathParts := strings.Split(name, "/")
	if strings.HasPrefix(name, "/") {
		pathParts = pathParts[1:]
	}

	switch len(pathParts) {
	case 3:
		return nopCloser{}, nil
	case 4:
		repo := pathParts[1]
		if pathParts[2] == "@v" {
			data := pathParts[3]
			dataExt := path.Ext(data)
			bookmark := pathParts[3][:len(pathParts[3])-len(dataExt)]
			switch dataExt {
			case ".info":
				pgTs, err := db.Q.GetPublicBookmarkTimeStamp(ctx, repo, bookmark)
				if err != nil {
					return nil, errors.Join(fmt.Errorf("failed to get timestamp for bookmark %q", bookmark), err)
				}
				return goproxyCacherGetInfo{
					Version: bookmark,
					Time:    pgTs.Time.Format(time.RFC3339),
				}.Reader(), nil
			case ".mod":
				goModHash, err := db.Q.GetPublicGoModByBookmark(ctx, repo, bookmark)
				if err != nil {
					return nil, errors.Join(fmt.Errorf("failed to get go.mod hash for bookmark %q", bookmark), err)
				}
				hashStr := base64.URLEncoding.EncodeToString(goModHash)
				f, err := filecontents.OpenFileByHash(hashStr)
				if err != nil {
					return nil, errors.Join(fmt.Errorf("failed to open go.mod file for bookmark %q", bookmark), err)
				}
				return f, nil
			case ".zip":
				vcsFiles, err := db.Q.GetPublicFileHashesByBookmark(ctx, repo, bookmark)
				if err != nil {
					return nil, errors.Join(fmt.Errorf("failed to get file hashes for bookmark %q", bookmark), err)
				}
				buf := new(bytes.Buffer)
				zipWriter := zip.NewWriter(buf)
				for _, vcsFile := range vcsFiles {
					hashStr := base64.URLEncoding.EncodeToString(vcsFile.ContentHash)
					f, err := filecontents.OpenFileByHash(hashStr)
					if err != nil {
						zipWriter.Close()
						return nil, errors.Join(fmt.Errorf("failed to open file %q for zip: %w", vcsFile.Name, err), err)
					}
					defer f.Close()

					zipEntryName := path.Join(fmt.Sprintf("%s/%s@%s", pathParts[0], repo, bookmark), vcsFile.Name)
					fileWriter, err := zipWriter.Create(zipEntryName)
					if err != nil {
						zipWriter.Close()
						return nil, errors.Join(fmt.Errorf("failed to create zip entry %q: %w", zipEntryName, err), err)
					}
					_, err = io.Copy(fileWriter, f)
					if err != nil {
						zipWriter.Close()
						return nil, errors.Join(fmt.Errorf("failed to copy file %q to zip entry: %w", vcsFile.Name, err), err)
					}
				}
				if err := zipWriter.Close(); err != nil {
					return nil, errors.Join(fmt.Errorf("failed to close zip archive: %w", err), err)
				}
				return io.NopCloser(buf), nil
			}
		}
	}

	return nil, fs.ErrNotExist
}

func (c *goproxyCacher) Put(ctx context.Context, name string, content io.ReadSeeker) error {
	return nil
}
