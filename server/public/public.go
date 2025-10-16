package public

import (
	"crypto/md5"
	"embed"
	"encoding/base64"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
)

var (
	//go:embed *.css
	assets  embed.FS
	fsToUrl = make(map[string]string)
	urlToFs = make(map[string]string)
)

func init() {
	err := fs.WalkDir(assets, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		f, err := assets.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		hasher := md5.New()
		if _, err := io.Copy(hasher, f); err != nil {
			return err
		}
		hash := base64.RawURLEncoding.EncodeToString(hasher.Sum(nil))
		url := "/public/" + hash
		fsToUrl[path] = url
		urlToFs[url] = path

		return nil
	})
	if err != nil {
		panic(err)
	}
}

func Handle(w http.ResponseWriter, r *http.Request) {
	if fsPath, ok := urlToFs[r.URL.Path]; ok {
		f, err := assets.Open(fsPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer f.Close()

		w.Header().Set("Content-Type", contentTypeFromFsPath(fsPath))
		w.Header().Set("Cache-Control", "public, max-age=2592000") // 30 days
		_, _ = io.Copy(w, f)
		return
	}

	http.Error(w, fmt.Sprintf("File not found: %s", r.URL.Path), http.StatusNotFound)
}

func Url(filename string) string {
	return fsToUrl[filename]
}

func contentTypeFromFsPath(fsPath string) string {
	switch path.Ext(fsPath) {
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".html":
		return "text/html"
	default:
		return "application/octet-stream"
	}
}
