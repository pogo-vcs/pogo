package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/pogo-vcs/pogo/auth"
)

// getHTTPScheme returns the HTTP scheme (http or https) based on whether TLS is used
func (c *Client) getHTTPScheme() string {
	// Detect TLS support using the same logic as gRPC connection
	supportsTLS, _ := detectTLSSupport(c.ctx, c.server)
	if supportsTLS {
		return "https"
	}
	return "http"
}

// makeHTTPRequest creates an HTTP request with authentication headers
func (c *Client) makeHTTPRequest(method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(c.ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	// Add authentication token
	token := auth.Encode(c.Token)
	req.Header.Set("Authorization", "Bearer "+token)

	return req, nil
}

// ListAssets returns a list of all assets for the current repository.
func (c *Client) ListAssets() ([]string, error) {
	scheme := c.getHTTPScheme()
	url := fmt.Sprintf("%s://%s/assets/%d/", scheme, c.server, c.repoId)

	req, err := c.makeHTTPRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.Join(errors.New("create request"), err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.Join(errors.New("send request"), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var assets []string
	if err := json.NewDecoder(resp.Body).Decode(&assets); err != nil {
		return nil, errors.Join(errors.New("decode response"), err)
	}

	return assets, nil
}

// GetAsset downloads an asset and writes it to the provided writer.
func (c *Client) GetAsset(name string, writer io.Writer) error {
	scheme := c.getHTTPScheme()
	url := fmt.Sprintf("%s://%s/assets/%d/%s", scheme, c.server, c.repoId, name)

	req, err := c.makeHTTPRequest(http.MethodGet, url, nil)
	if err != nil {
		return errors.Join(errors.New("create request"), err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.Join(errors.New("send request"), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	_, err = io.Copy(writer, resp.Body)
	if err != nil {
		return errors.Join(errors.New("read response"), err)
	}

	return nil
}

// PutAsset uploads an asset from the provided reader.
func (c *Client) PutAsset(name string, reader io.Reader) error {
	scheme := c.getHTTPScheme()
	url := fmt.Sprintf("%s://%s/assets/%d/%s", scheme, c.server, c.repoId, name)

	req, err := c.makeHTTPRequest(http.MethodPut, url, reader)
	if err != nil {
		return errors.Join(errors.New("create request"), err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.Join(errors.New("send request"), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}

// DeleteAsset deletes an asset.
func (c *Client) DeleteAsset(name string) error {
	scheme := c.getHTTPScheme()
	url := fmt.Sprintf("%s://%s/assets/%d/%s", scheme, c.server, c.repoId, name)

	req, err := c.makeHTTPRequest(http.MethodDelete, url, nil)
	if err != nil {
		return errors.Join(errors.New("create request"), err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.Join(errors.New("send request"), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}
