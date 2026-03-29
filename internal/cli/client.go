package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/api"
)

type apiClient struct {
	baseURL    string
	token      string
	apiKey     string
	httpClient *http.Client
}

type listResponse[T any] struct {
	Data   []T `json:"data"`
	Total  int `json:"total,omitempty"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

func newAPIClient(baseURL, token, apiKey string) *apiClient {
	return &apiClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
		apiKey:  strings.TrimSpace(apiKey),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *apiClient) get(ctx context.Context, path string, query url.Values, dst any) error {
	req, err := c.newRequest(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return err
	}
	return c.do(req, dst)
}

func (c *apiClient) post(ctx context.Context, path string, query url.Values, body, dst any) error {
	req, err := c.newRequest(ctx, http.MethodPost, path, query, body)
	if err != nil {
		return err
	}
	return c.do(req, dst)
}

func (c *apiClient) newRequest(ctx context.Context, method, path string, query url.Values, body any) (*http.Request, error) {
	endpoint, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("build request url: %w", err)
	}
	if len(query) > 0 {
		endpoint.RawQuery = query.Encode()
	}

	var payload *bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		payload = bytes.NewReader(encoded)
	} else {
		payload = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), payload)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	return req, nil
}

func (c *apiClient) do(req *http.Request, dst any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		var apiErr api.ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err == nil && apiErr.Error != "" {
			return fmt.Errorf("%s %s: %s (%s)", req.Method, req.URL.Path, apiErr.Error, apiErr.Code)
		}
		return fmt.Errorf("%s %s: unexpected status %s", req.Method, req.URL.Path, resp.Status)
	}
	if dst == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("%s %s: decode response: %w", req.Method, req.URL.Path, err)
	}
	return nil
}
