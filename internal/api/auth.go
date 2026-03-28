package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

const (
	accessTokenType        = "access"
	refreshTokenType       = "refresh"
	defaultRefreshTokenTTL = 24 * time.Hour
	defaultAPIKeyPrefix    = "grq"
)

var (
	errMissingJWTSecret   = errors.New("jwt secret is required")
	errInvalidToken       = errors.New("invalid token")
	errExpiredToken       = errors.New("token expired")
	errInvalidAPIKey      = errors.New("invalid api key")
	errAPIKeyRevoked      = errors.New("api key revoked")
	errAPIKeyExpired      = errors.New("api key expired")
	errMissingCredentials = errors.New("missing credentials")
)

type authContextKey string

const authPrincipalContextKey authContextKey = "auth_principal"

// AuthPrincipal describes the caller authenticated by middleware.
type AuthPrincipal struct {
	Subject  string
	AuthType string
	APIKeyID uuid.UUID
}

// AuthResult contains the authenticated principal and any matched API key.
type AuthResult struct {
	Principal AuthPrincipal
	APIKey    *domain.APIKey
}

// TokenPair groups access and refresh tokens minted together.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// AuthConfig defines JWT and API key behavior for the API server.
type AuthConfig struct {
	JWTSecret       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	APIKeys         repository.APIKeyRepository
	APIKeyRateLimit int
	APIKeyWindow    time.Duration
}

// DefaultAuthConfig returns the default auth configuration.
func DefaultAuthConfig() AuthConfig {
	return AuthConfig{
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: defaultRefreshTokenTTL,
		APIKeyRateLimit: 100,
		APIKeyWindow:    time.Minute,
	}
}

// AuthManager issues and validates JWTs and API keys.
type AuthManager struct {
	secret          []byte
	apiKeys         repository.APIKeyRepository
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
	nowFunc         func() time.Time
	keyLimiter      *TokenBucketRateLimiter
	defaultKeyLimit int
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type jwtClaims struct {
	Subject   string `json:"sub"`
	TokenType string `json:"token_type"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

// TokenBucketRateLimiter implements a token-bucket limiter keyed by identifier.
type TokenBucketRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	window  time.Duration
	nowFunc func() time.Time
}

type tokenBucket struct {
	tokens   float64
	last     time.Time
	capacity int
}

// NewTokenBucketRateLimiter creates a per-key token-bucket limiter.
func NewTokenBucketRateLimiter(window time.Duration) *TokenBucketRateLimiter {
	if window <= 0 {
		window = time.Minute
	}
	return &TokenBucketRateLimiter{
		buckets: make(map[string]*tokenBucket),
		window:  window,
		nowFunc: time.Now,
	}
}

// Allow returns true when the key has at least one token available.
func (rl *TokenBucketRateLimiter) Allow(key string, limit int) bool {
	if limit <= 0 {
		return true
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.nowFunc()
	bucket, ok := rl.buckets[key]
	if !ok || bucket.capacity != limit {
		rl.buckets[key] = &tokenBucket{
			tokens:   float64(limit - 1),
			last:     now,
			capacity: limit,
		}
		return true
	}

	ratePerSecond := float64(limit) / rl.window.Seconds()
	elapsed := now.Sub(bucket.last).Seconds()
	if elapsed > 0 {
		bucket.tokens += elapsed * ratePerSecond
		if bucket.tokens > float64(limit) {
			bucket.tokens = float64(limit)
		}
		bucket.last = now
	}

	if bucket.tokens < 1 {
		return false
	}

	bucket.tokens--
	return true
}

// NewAuthManager creates a new auth manager from server configuration.
func NewAuthManager(cfg AuthConfig) (*AuthManager, error) {
	cfg = applyDefaultAuthConfig(cfg)
	if strings.TrimSpace(cfg.JWTSecret) == "" {
		return nil, errMissingJWTSecret
	}

	return &AuthManager{
		secret:          []byte(cfg.JWTSecret),
		apiKeys:         cfg.APIKeys,
		accessTokenTTL:  cfg.AccessTokenTTL,
		refreshTokenTTL: cfg.RefreshTokenTTL,
		nowFunc:         time.Now,
		keyLimiter:      NewTokenBucketRateLimiter(cfg.APIKeyWindow),
		defaultKeyLimit: cfg.APIKeyRateLimit,
	}, nil
}

func applyDefaultAuthConfig(cfg AuthConfig) AuthConfig {
	defaults := DefaultAuthConfig()
	if cfg.AccessTokenTTL <= 0 {
		cfg.AccessTokenTTL = defaults.AccessTokenTTL
	}
	if cfg.RefreshTokenTTL <= 0 {
		cfg.RefreshTokenTTL = defaults.RefreshTokenTTL
	}
	if cfg.APIKeyRateLimit <= 0 {
		cfg.APIKeyRateLimit = defaults.APIKeyRateLimit
	}
	if cfg.APIKeyWindow <= 0 {
		cfg.APIKeyWindow = defaults.APIKeyWindow
	}
	return cfg
}

// GenerateTokenPair creates a short-lived access token and a refresh token.
func (a *AuthManager) GenerateTokenPair(subject string) (TokenPair, error) {
	accessToken, expiresAt, err := a.generateJWT(subject, accessTokenType, a.accessTokenTTL)
	if err != nil {
		return TokenPair{}, err
	}
	refreshToken, _, err := a.generateJWT(subject, refreshTokenType, a.refreshTokenTTL)
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
	}, nil
}

// RefreshTokenPair validates a refresh token and returns a new token pair.
func (a *AuthManager) RefreshTokenPair(refreshToken string) (TokenPair, error) {
	claims, err := a.validateJWT(refreshToken, refreshTokenType)
	if err != nil {
		return TokenPair{}, err
	}
	return a.GenerateTokenPair(claims.Subject)
}

// ValidateAccessToken validates a bearer token and returns the authenticated principal.
func (a *AuthManager) ValidateAccessToken(token string) (AuthPrincipal, error) {
	claims, err := a.validateJWT(token, accessTokenType)
	if err != nil {
		return AuthPrincipal{}, err
	}
	return AuthPrincipal{
		Subject:  claims.Subject,
		AuthType: accessTokenType,
	}, nil
}

// CreateAPIKey generates a new API key, stores only its hash, and returns the plaintext once.
func (a *AuthManager) CreateAPIKey(ctx context.Context, name string, expiresAt *time.Time) (string, *domain.APIKey, error) {
	if a.apiKeys == nil {
		return "", nil, fmt.Errorf("api key repository is required")
	}

	plaintext, prefix, err := generateAPIKey()
	if err != nil {
		return "", nil, err
	}

	key := &domain.APIKey{
		Name:               name,
		KeyPrefix:          prefix,
		KeyHash:            hashAPIKey(plaintext),
		RateLimitPerMinute: a.defaultKeyLimit,
		ExpiresAt:          expiresAt,
	}
	if err := a.apiKeys.Create(ctx, key); err != nil {
		return "", nil, err
	}
	return plaintext, key, nil
}

// AuthenticateRequest validates either a bearer token or API key from the request.
func (a *AuthManager) AuthenticateRequest(r *http.Request) (AuthResult, error) {
	if token := bearerTokenFromHeader(r.Header.Get("Authorization")); token != "" {
		principal, err := a.ValidateAccessToken(token)
		if err != nil {
			return AuthResult{}, err
		}
		return AuthResult{Principal: principal}, nil
	}

	if rawKey := strings.TrimSpace(r.Header.Get("X-API-Key")); rawKey != "" {
		return a.validateAPIKey(r.Context(), rawKey)
	}

	return AuthResult{}, errMissingCredentials
}

// PrincipalFromContext returns the authenticated principal attached by middleware.
func PrincipalFromContext(ctx context.Context) (AuthPrincipal, bool) {
	principal, ok := ctx.Value(authPrincipalContextKey).(AuthPrincipal)
	return principal, ok
}

func (a *AuthManager) validateAPIKey(ctx context.Context, rawKey string) (AuthResult, error) {
	if a.apiKeys == nil {
		return AuthResult{}, errInvalidAPIKey
	}

	prefix, err := apiKeyPrefix(rawKey)
	if err != nil {
		return AuthResult{}, errInvalidAPIKey
	}

	key, err := a.apiKeys.GetByPrefix(ctx, prefix)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return AuthResult{}, errInvalidAPIKey
		}
		return AuthResult{}, err
	}

	if key.RevokedAt != nil && !key.RevokedAt.After(a.nowFunc()) {
		return AuthResult{}, errAPIKeyRevoked
	}
	if key.ExpiresAt != nil && !key.ExpiresAt.After(a.nowFunc()) {
		return AuthResult{}, errAPIKeyExpired
	}
	if !verifyAPIKey(rawKey, key.KeyHash) {
		return AuthResult{}, errInvalidAPIKey
	}

	_ = a.apiKeys.TouchLastUsed(ctx, key.ID, a.nowFunc())

	return AuthResult{
		Principal: AuthPrincipal{
			Subject:  key.Name,
			AuthType: "api_key",
			APIKeyID: key.ID,
		},
		APIKey: key,
	}, nil
}

func (a *AuthManager) generateJWT(subject, tokenType string, ttl time.Duration) (string, time.Time, error) {
	if strings.TrimSpace(subject) == "" {
		return "", time.Time{}, fmt.Errorf("subject is required")
	}

	issuedAt := a.nowFunc().UTC()
	expiresAt := issuedAt.Add(ttl)

	header := jwtHeader{Alg: "HS256", Typ: "JWT"}
	claims := jwtClaims{
		Subject:   subject,
		TokenType: tokenType,
		IssuedAt:  issuedAt.Unix(),
		ExpiresAt: expiresAt.Unix(),
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("marshal jwt header: %w", err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("marshal jwt claims: %w", err)
	}

	encodedHeader := base64.RawURLEncoding.EncodeToString(headerJSON)
	encodedClaims := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := encodedHeader + "." + encodedClaims
	signature := signJWT(signingInput, a.secret)

	return signingInput + "." + signature, expiresAt, nil
}

func (a *AuthManager) validateJWT(token, expectedType string) (jwtClaims, error) {
	var claims jwtClaims

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, errInvalidToken
	}

	signingInput := parts[0] + "." + parts[1]
	expectedSig := signJWT(signingInput, a.secret)
	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return claims, errInvalidToken
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims, errInvalidToken
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return claims, errInvalidToken
	}
	if claims.TokenType != expectedType {
		return claims, errInvalidToken
	}
	if a.nowFunc().Unix() >= claims.ExpiresAt {
		return claims, errExpiredToken
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return claims, errInvalidToken
	}

	return claims, nil
}

func signJWT(signingInput string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(signingInput))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func bearerTokenFromHeader(header string) string {
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return ""
	}
	return strings.TrimSpace(header[len("Bearer "):])
}

func generateAPIKey() (string, string, error) {
	prefixBytes := make([]byte, 6)
	secretBytes := make([]byte, 24)
	if _, err := rand.Read(prefixBytes); err != nil {
		return "", "", fmt.Errorf("generate api key prefix: %w", err)
	}
	if _, err := rand.Read(secretBytes); err != nil {
		return "", "", fmt.Errorf("generate api key secret: %w", err)
	}

	prefix := defaultAPIKeyPrefix + "_" + strings.ToLower(hex.EncodeToString(prefixBytes))
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	return prefix + "." + secret, prefix, nil
}

func apiKeyPrefix(raw string) (string, error) {
	prefix, _, ok := strings.Cut(strings.TrimSpace(raw), ".")
	if !ok || strings.TrimSpace(prefix) == "" {
		return "", errInvalidAPIKey
	}
	return prefix, nil
}

func hashAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func verifyAPIKey(raw, expectedHash string) bool {
	return hmac.Equal([]byte(hashAPIKey(raw)), []byte(expectedHash))
}
