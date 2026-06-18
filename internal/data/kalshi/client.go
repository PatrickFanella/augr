package kalshi

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://external-api.demo.kalshi.co/trade-api/v2"
	defaultTimeout = 30 * time.Second
)

// Client is a small HTTP client for Kalshi Trade API v2.
type Client struct {
	baseURL    string
	apiKeyID   string
	privateKey *rsa.PrivateKey
	httpClient *http.Client
	now        func() time.Time
	logger     *slog.Logger
}

// NewClient constructs a Kalshi HTTP client.
// If logger is nil, slog.Default() is used.
func NewClient(baseURL, apiKeyID, privateKeyPEMB64 string, logger *slog.Logger) (*Client, error) {
	if logger == nil {
		logger = slog.Default()
	}

	trimmedBaseURL := strings.TrimSpace(baseURL)
	if trimmedBaseURL == "" {
		trimmedBaseURL = defaultBaseURL
	}

	privateKey, err := parsePrivateKey(strings.TrimSpace(privateKeyPEMB64))
	if err != nil {
		return nil, fmt.Errorf("kalshi: parse private key: %w", err)
	}

	client := &Client{
		baseURL:  trimmedBaseURL,
		apiKeyID: strings.TrimSpace(apiKeyID),
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		now:    time.Now,
		logger: logger,
	}
	if privateKey != nil {
		client.privateKey = privateKey
	}

	return client, nil
}

// SetHTTPClient replaces the underlying HTTP client. This is primarily useful for testing.
func (c *Client) SetHTTPClient(httpClient *http.Client) {
	if c == nil || httpClient == nil {
		return
	}
	c.httpClient = httpClient
}

func (c *Client) setNowFunc(now func() time.Time) {
	if c == nil || now == nil {
		return
	}
	c.now = now
}

// Get issues a GET request and returns the raw response body.
func (c *Client) Get(ctx context.Context, path string, query url.Values, authenticated bool) ([]byte, error) {
	return c.do(ctx, http.MethodGet, path, query, nil, authenticated)
}

// Post issues an authenticated POST request with a JSON body.
func (c *Client) Post(ctx context.Context, path string, body any) ([]byte, error) {
	return c.do(ctx, http.MethodPost, path, nil, body, true)
}

func (c *Client) do(ctx context.Context, method, requestPath string, query url.Values, body any, authenticated bool) ([]byte, error) {
	if c == nil {
		return nil, errors.New("kalshi: client is nil")
	}

	if authenticated {
		if strings.TrimSpace(c.apiKeyID) == "" {
			return nil, errors.New("kalshi: api key id is required")
		}
		if c.privateKey == nil {
			return nil, errors.New("kalshi: private key is required")
		}
	}

	requestURL, signedPath, err := c.buildURL(requestPath, query)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := marshalBody(body)
	if err != nil {
		return nil, fmt.Errorf("kalshi: marshal request body: %w", err)
	}
	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytesReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("kalshi: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}

	if authenticated {
		headers, err := c.authHeaders(method, signedPath)
		if err != nil {
			return nil, err
		}
		for key, values := range headers {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
	}

	resp, err := c.getHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("kalshi: do request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("kalshi: read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("kalshi: request failed (status=%d): %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	return responseBody, nil
}

func (c *Client) authHeaders(method, signedPath string) (http.Header, error) {
	timestamp := fmt.Sprintf("%d", c.now().UnixMilli())
	message := signingMessage(timestamp, method, signedPath)
	signature, err := signMessage(c.privateKey, []byte(message))
	if err != nil {
		return nil, err
	}

	headers := make(http.Header, 3)
	headers.Set("KALSHI-ACCESS-KEY", c.apiKeyID)
	headers.Set("KALSHI-ACCESS-TIMESTAMP", timestamp)
	headers.Set("KALSHI-ACCESS-SIGNATURE", signature)
	return headers, nil
}

func (c *Client) buildURL(requestPath string, query url.Values) (string, string, error) {
	baseURL, err := url.Parse(c.baseURL)
	if err != nil {
		return "", "", fmt.Errorf("kalshi: parse base url: %w", err)
	}

	joinedPath := joinPath(baseURL.Path, requestPath)
	baseURL.Path = joinedPath
	baseURL.RawPath = ""
	if len(query) > 0 {
		baseURL.RawQuery = query.Encode()
	}

	return baseURL.String(), joinedPath, nil
}

func (c *Client) getHTTPClient() *http.Client {
	if c.httpClient != nil {
		return c.httpClient
	}
	return &http.Client{Timeout: defaultTimeout}
}

func joinPath(basePath, requestPath string) string {
	trimmedPath := strings.TrimSpace(requestPath)
	if trimmedPath == "" {
		if basePath == "" {
			return "/"
		}
		return basePath
	}

	cleanPath := "/" + strings.TrimLeft(trimmedPath, "/")
	if basePath == "" || basePath == "/" {
		return cleanPath
	}
	return strings.TrimRight(basePath, "/") + cleanPath
}

func signingMessage(timestamp, method, signedPath string) string {
	return timestamp + strings.ToUpper(method) + signedPath
}

func signMessage(privateKey *rsa.PrivateKey, message []byte) (string, error) {
	hash := sha256.Sum256(message)
	sig, err := rsa.SignPSS(rand.Reader, privateKey, crypto.SHA256, hash[:], &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash, Hash: crypto.SHA256})
	if err != nil {
		return "", fmt.Errorf("sign request: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

func marshalBody(body any) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	return json.Marshal(body)
}

func parsePrivateKey(privateKeyPEMB64 string) (*rsa.PrivateKey, error) {
	if privateKeyPEMB64 == "" {
		return nil, nil
	}

	decoded, err := base64.StdEncoding.DecodeString(privateKeyPEMB64)
	if err != nil {
		return nil, fmt.Errorf("decode base64 pem: %w", err)
	}

	block, _ := pem.Decode(decoded)
	if block == nil {
		return nil, errors.New("decode pem block: invalid PEM")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("parse private key: not an RSA private key")
		}
		return rsaKey, nil
	}

	rsaKey, rsaErr := x509.ParsePKCS1PrivateKey(block.Bytes)
	if rsaErr != nil {
		return nil, fmt.Errorf("parse rsa private key: %w", rsaErr)
	}
	return rsaKey, nil
}

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

// QuoteCentsToProbability converts a Kalshi quote in cents to a probability.
// It accepts values in [0,100] and rejects NaN/Inf and out-of-range inputs.
func QuoteCentsToProbability(cents float64) (float64, error) {
	if math.IsNaN(cents) || math.IsInf(cents, 0) || cents < 0 || cents > 100 {
		return 0, fmt.Errorf("kalshi: quote cents %.4f out of range [0,100]", cents)
	}
	return cents / 100, nil
}
