// Package mlclient contains the contract-shaped HTTP client for the ML service.
package mlclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func New(baseURL string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), httpClient: &http.Client{Timeout: 65 * time.Second}}
}

// NewWithHTTPClient is used by deterministic unit tests and local adapters.
func NewWithHTTPClient(baseURL string, httpClient *http.Client) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient}
}

func (c *Client) post(ctx context.Context, path string, request any, response any) error {
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal ML request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create ML request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ML request %s: %w", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("read ML response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ML %s returned %d: %s", path, resp.StatusCode, string(data))
	}
	if err := json.Unmarshal(data, response); err != nil {
		return fmt.Errorf("decode ML response: %w", err)
	}
	return nil
}

func (c *Client) Extract(ctx context.Context, request any, response any) error {
	return c.post(ctx, "/v1/extract", request, response)
}
func (c *Client) Embed(ctx context.Context, request any, response any) error {
	return c.post(ctx, "/v1/embed", request, response)
}
func (c *Client) Optimize(ctx context.Context, request any, response any) error {
	return c.post(ctx, "/v1/optimize", request, response)
}
