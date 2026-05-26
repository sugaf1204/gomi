package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// apiClient handles authenticated HTTP requests to the GOMI server.
type apiClient struct {
	serverURL string
	token     string
}

func newAPIClient(serverURL, token string) *apiClient {
	return &apiClient{
		serverURL: strings.TrimRight(serverURL, "/"),
		token:     token,
	}
}

func (c *apiClient) doRequest(ctx context.Context, method, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return http.DefaultClient.Do(req)
}

func (c *apiClient) fetchImages(ctx context.Context) ([]OSImage, error) {
	url := c.serverURL + "/api/v1/os-images"
	resp, err := c.doRequest(ctx, http.MethodGet, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	var result struct {
		Items []OSImage `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (c *apiClient) downloadImage(ctx context.Context, name, destPath string) error {
	url := c.serverURL + "/api/v1/os-images/" + name + "/download"
	resp, err := c.doRequest(ctx, http.MethodGet, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("download %s: status %d", name, resp.StatusCode)
	}
	return atomicWriteFromReader(destPath, resp.Body)
}
