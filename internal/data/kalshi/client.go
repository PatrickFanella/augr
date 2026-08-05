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
	mathrand "math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/metrics"
	provgov "github.com/PatrickFanella/get-rich-quick/internal/providergovernor"
)

type governor interface {
	Reserve(context.Context) error
	Sleep(context.Context, time.Duration) error
}

const (
	defaultBaseURL = "https://external-api.demo.kalshi.co/trade-api/v2"
	defaultTimeout = 30 * time.Second
)

// Client is a small HTTP client for Kalshi Trade API v2.
type Client struct {
	baseURL     string
	apiKeyID    string
	privateKey  *rsa.PrivateKey
	httpClient  *http.Client
	now         func() time.Time
	logger      *slog.Logger
	governor    governor
	metrics     *metrics.Metrics
	clientType  string
	nowClock    func() time.Time
	random      *mathrand.Rand
	randomMu    sync.Mutex
	sleeper     func(context.Context, time.Duration) error
	maxAttempts int
	baseBackoff time.Duration
	maxBackoff  time.Duration
	jitterRatio float64
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

	trimmedAPIKeyID := strings.TrimSpace(apiKeyID)
	trimmedPrivateKeyPEMB64 := strings.TrimSpace(privateKeyPEMB64)
	var privateKey *rsa.PrivateKey
	if trimmedAPIKeyID != "" || trimmedPrivateKeyPEMB64 != "" {
		var err error
		privateKey, err = parsePrivateKey(trimmedPrivateKeyPEMB64)
		if err != nil {
			return nil, fmt.Errorf("kalshi: parse private key: %w", err)
		}
	}

	client := &Client{
		baseURL:  trimmedBaseURL,
		apiKeyID: trimmedAPIKeyID,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		now:         time.Now,
		logger:      logger,
		nowClock:    time.Now,
		random:      mathrand.New(mathrand.NewSource(1)),
		sleeper:     func(ctx context.Context, d time.Duration) error { return (&provgov.ProviderGovernor{}).Sleep(ctx, d) },
		maxAttempts: 3,
		baseBackoff: 100 * time.Millisecond,
		maxBackoff:  2 * time.Second,
		jitterRatio: 0.2,
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

func (c *Client) SetGovernor(g governor) {
	if c != nil {
		c.governor = g
	}
}

func (c *Client) SetMetrics(m *metrics.Metrics, clientType string) {
	if c != nil {
		c.metrics = m
		c.clientType = clientType
	}
}

func (c *Client) ClientType() string {
	if c == nil {
		return ""
	}
	return c.clientType
}

func (c *Client) Governor() governor {
	if c == nil {
		return nil
	}
	return c.governor
}

// SetRetryPolicy configures retry attempts and the maximum retry wait.
// Retry-After is always respected as a floor. If a server asks for longer than
// max, the client returns the typed rate-limit error immediately so the caller
// or scheduler can defer without sleeping for hours.
func (c *Client) SetRetryPolicy(maxAttempts int, base, maximum time.Duration, jitter float64) {
	if c != nil {
		c.maxAttempts, c.baseBackoff, c.maxBackoff, c.jitterRatio = maxAttempts, base, maximum, jitter
	}
}

func (c *Client) SetHooks(now func() time.Time, random func() float64, sleeper func(context.Context, time.Duration) error) {
	if c != nil {
		if now != nil {
			c.nowClock = now
		}
		if random != nil {
			c.randomMu.Lock()
			c.random = mathrand.New(mathrand.NewSource(int64(random()*1e9) + 1))
			c.randomMu.Unlock()
		}
		if sleeper != nil {
			c.sleeper = sleeper
		}
	}
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

// Delete issues an authenticated DELETE request.
func (c *Client) Delete(ctx context.Context, path string, query url.Values) ([]byte, error) {
	return c.do(ctx, http.MethodDelete, path, query, nil, true)
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

	if method == http.MethodGet || method == http.MethodDelete {
		return c.doWithRetry(ctx, req, method, authenticated)
	}
	if c.governor != nil {
		if err := c.governor.Reserve(ctx); err != nil {
			return nil, err
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
		if resp.StatusCode == 429 {
			if c.metrics != nil {
				c.metrics.RecordKalshiRateLimit("kalshi", c.clientType, method)
			}
			retryAfter := provgov.ParseRetryAfter(resp.Header.Get("Retry-After"), c.nowClock)
			if retryAfter > 0 {
				if err := c.persistCooldown(ctx, retryAfter); err != nil {
					return nil, errors.Join(provgov.Wrap("kalshi", c.clientType, method, resp.StatusCode, retryAfter, string(responseBody)), err)
				}
			}
			return nil, provgov.Wrap("kalshi", c.clientType, method, resp.StatusCode, retryAfter, string(responseBody))
		}
		return nil, fmt.Errorf("kalshi: request failed (status=%d): %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	return responseBody, nil
}

func (c *Client) doWithRetry(ctx context.Context, req *http.Request, method string, authenticated bool) ([]byte, error) {
	_ = authenticated
	maxAttempts := provgov.MaxAttempts(c.maxAttempts)
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if c.governor != nil {
			if err := c.governor.Reserve(ctx); err != nil {
				return nil, err
			}
		}
		resp, err := c.getHTTPClient().Do(req.Clone(ctx))
		if err != nil {
			return nil, fmt.Errorf("kalshi: do request: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode == 429 {
			retryAfter := provgov.ParseRetryAfter(resp.Header.Get("Retry-After"), c.nowClock)
			if c.metrics != nil {
				c.metrics.RecordKalshiRateLimit("kalshi", c.clientType, method)
			}
			if retryAfter > 0 {
				if err := c.persistCooldown(ctx, retryAfter); err != nil {
					return nil, errors.Join(provgov.Wrap("kalshi", c.clientType, method, resp.StatusCode, retryAfter, string(body)), err)
				}
			}
			if retryAfter > c.maxBackoff && c.maxBackoff > 0 {
				return nil, provgov.Wrap("kalshi", c.clientType, method, resp.StatusCode, retryAfter, string(body))
			}
			if attempt+1 < maxAttempts && method != http.MethodPost {
				wait := c.nextBackoff(attempt, retryAfter)
				if err := c.recordRetryAndSleep(ctx, method, wait); err != nil {
					return nil, err
				}
				continue
			}
			return nil, provgov.Wrap("kalshi", c.clientType, method, resp.StatusCode, retryAfter, string(body))
		}
		if resp.StatusCode >= 500 && method != http.MethodPost {
			if attempt+1 < maxAttempts {
				if err := c.recordRetryAndSleep(ctx, method, c.nextBackoff(attempt, 0)); err != nil {
					return nil, err
				}
				continue
			}
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("kalshi: request failed (status=%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return body, nil
	}
	return nil, errors.New("kalshi: retry exhausted")
}

func (c *Client) persistCooldown(ctx context.Context, retryAfter time.Duration) error {
	gov, ok := c.governor.(*provgov.ProviderGovernor)
	if !ok || gov == nil || gov.Cooldown == nil {
		return nil
	}
	now := c.nowClock
	if now == nil {
		now = time.Now
	}
	return gov.Cooldown.SetProviderCooldown(ctx, gov.Provider, now().Add(retryAfter))
}

func (c *Client) recordRetryAndSleep(ctx context.Context, method string, wait time.Duration) error {
	if c.metrics != nil {
		c.metrics.RecordKalshiRetryAttempt("kalshi", c.clientType, method)
		c.metrics.ObserveKalshiRetryWaitSeconds("kalshi", c.clientType, method, wait.Seconds())
	}
	return c.sleep(ctx, wait)
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.governor != nil {
		return c.governor.Sleep(ctx, d)
	}
	return c.sleeper(ctx, d)
}

func (c *Client) nextBackoff(attempt int, retryAfter time.Duration) time.Duration {
	b := c.baseBackoff
	if b <= 0 {
		b = 100 * time.Millisecond
	}
	for range max(attempt, 0) {
		if c.maxBackoff > 0 && b >= c.maxBackoff/2 {
			b = c.maxBackoff
			break
		}
		if b > time.Duration(1<<63-1)/2 {
			b = time.Duration(1<<63 - 1)
			break
		}
		b *= 2
	}
	if c.maxBackoff > 0 && b > c.maxBackoff {
		b = c.maxBackoff
	}
	if c.jitterRatio > 0 {
		b = c.jitter(b)
		if c.maxBackoff > 0 && b > c.maxBackoff {
			b = c.maxBackoff
		}
	}
	if retryAfter > b {
		b = retryAfter
	}
	return b
}

func (c *Client) jitter(base time.Duration) time.Duration {
	if c == nil || base <= 0 || c.jitterRatio <= 0 {
		return base
	}
	c.randomMu.Lock()
	r := c.random
	var x float64
	if r == nil {
		x = 0.5
	} else {
		x = r.Float64()
	}
	c.randomMu.Unlock()
	offset := (x*2 - 1) * c.jitterRatio
	return time.Duration(float64(base) * (1 + offset))
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
