package client

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
)

type fileUploadInfo struct {
	hash    []byte
	absPath string
	name    string
}

// uploadFileHTTP uploads a single file via HTTP PUT to /v1/objects/{hash}.
func (c *Client) uploadFileHTTP(httpClient *http.Client, baseURL string, info fileUploadInfo) error {
	hashStr := base64.URLEncoding.EncodeToString(info.hash)
	url := baseURL + hashStr

	f, err := os.Open(info.absPath)
	if err != nil {
		return fmt.Errorf("open file %s: %w", info.name, err)
	}
	defer f.Close()

	req, err := c.makeHTTPRequest(http.MethodPut, url, f)
	if err != nil {
		return fmt.Errorf("create request for %s: %w", info.name, err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload %s: %w", info.name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("upload %s: server returned %d: %s", info.name, resp.StatusCode, strings.TrimSpace(string(body)))
}

// uploadFilesHTTP uploads multiple files via HTTP PUT with bounded concurrency.
func (c *Client) uploadFilesHTTP(ctx context.Context, files []fileUploadInfo) error {
	if len(files) == 0 {
		return nil
	}

	scheme := c.getHTTPScheme()
	baseURL := fmt.Sprintf("%s://%s/v1/objects/", scheme, c.getServer())
	httpClient := &http.Client{}

	const maxConcurrency = 8

	var (
		wg      sync.WaitGroup
		errOnce sync.Once
		firstErr error
	)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, maxConcurrency)

	for _, file := range files {
		// Check cancellation before acquiring semaphore
		if ctx.Err() != nil {
			break
		}

		select {
		case <-ctx.Done():
			continue
		case sem <- struct{}{}:
		}

		if ctx.Err() != nil {
			<-sem
			break
		}

		wg.Add(1)
		go func(info fileUploadInfo) {
			defer wg.Done()
			defer func() { <-sem }()

			if ctx.Err() != nil {
				return
			}

			if err := c.uploadFileHTTP(httpClient, baseURL, info); err != nil {
				errOnce.Do(func() {
					firstErr = err
					cancel()
				})
				return
			}
			fmt.Fprintf(c.VerboseOut, "Uploaded: %s\n", info.name)
		}(file)
	}

	wg.Wait()
	return firstErr
}
