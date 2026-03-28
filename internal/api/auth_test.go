package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuthManagerGenerateAndValidateAccessToken(t *testing.T) {
	t.Parallel()

	auth, err := NewAuthManager(AuthConfig{JWTSecret: "test-secret"})
	if err != nil {
		t.Fatalf("NewAuthManager() error = %v", err)
	}

	now := time.Date(2026, 3, 28, 1, 30, 0, 0, time.UTC)
	auth.nowFunc = func() time.Time { return now }

	pair, err := auth.GenerateTokenPair("alice")
	if err != nil {
		t.Fatalf("GenerateTokenPair() error = %v", err)
	}

	principal, err := auth.ValidateAccessToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}
	if principal.Subject != "alice" {
		t.Fatalf("principal.Subject = %q, want %q", principal.Subject, "alice")
	}

	now = now.Add(time.Hour + time.Second)
	if _, err := auth.ValidateAccessToken(pair.AccessToken); !errors.Is(err, errExpiredToken) {
		t.Fatalf("ValidateAccessToken() error = %v, want expired token", err)
	}
}

func TestAuthManagerRefreshTokenPair(t *testing.T) {
	t.Parallel()

	auth, err := NewAuthManager(AuthConfig{JWTSecret: "test-secret"})
	if err != nil {
		t.Fatalf("NewAuthManager() error = %v", err)
	}

	auth.nowFunc = func() time.Time {
		return time.Date(2026, 3, 28, 1, 30, 0, 0, time.UTC)
	}

	pair, err := auth.GenerateTokenPair("alice")
	if err != nil {
		t.Fatalf("GenerateTokenPair() error = %v", err)
	}

	refreshed, err := auth.RefreshTokenPair(pair.RefreshToken)
	if err != nil {
		t.Fatalf("RefreshTokenPair() error = %v", err)
	}

	principal, err := auth.ValidateAccessToken(refreshed.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}
	if principal.Subject != "alice" {
		t.Fatalf("principal.Subject = %q, want %q", principal.Subject, "alice")
	}
}

func TestAuthManagerCreateAndValidateAPIKey(t *testing.T) {
	t.Parallel()

	repo := newStubAPIKeyRepo()
	auth, err := NewAuthManager(AuthConfig{
		JWTSecret: "test-secret",
		APIKeys:   repo,
	})
	if err != nil {
		t.Fatalf("NewAuthManager() error = %v", err)
	}

	rawKey, storedKey, err := auth.CreateAPIKey(context.Background(), "integration-bot", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/strategies", nil)
	req.Header.Set("X-API-Key", rawKey)

	result, err := auth.AuthenticateRequest(req)
	if err != nil {
		t.Fatalf("AuthenticateRequest() error = %v", err)
	}
	if result.Principal.AuthType != "api_key" {
		t.Fatalf("result.Principal.AuthType = %q, want %q", result.Principal.AuthType, "api_key")
	}
	if result.Principal.APIKeyID != storedKey.ID {
		t.Fatalf("result.Principal.APIKeyID = %s, want %s", result.Principal.APIKeyID, storedKey.ID)
	}
}

func TestProtectedEndpointRequiresAuthentication(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/strategies", nil)
	rr := httptest.NewRecorder()

	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestProtectedEndpointAcceptsAPIKey(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	rawKey, _, err := srv.auth.CreateAPIKey(context.Background(), "service-account", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/strategies", nil)
	req.Header.Set("X-API-Key", rawKey)
	rr := httptest.NewRecorder()

	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\nbody: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

func TestAPIKeyRateLimiterIsPerKey(t *testing.T) {
	t.Parallel()

	rl := NewTokenBucketRateLimiter(time.Minute)
	now := time.Date(2026, 3, 28, 1, 30, 0, 0, time.UTC)
	rl.nowFunc = func() time.Time { return now }

	if !rl.Allow("key-a", 1) {
		t.Fatal("first request for key-a should be allowed")
	}
	if rl.Allow("key-a", 1) {
		t.Fatal("second request for key-a should be limited")
	}
	if !rl.Allow("key-b", 1) {
		t.Fatal("first request for key-b should be allowed independently")
	}

	now = now.Add(time.Minute)
	if !rl.Allow("key-a", 1) {
		t.Fatal("key-a should be refilled after one minute")
	}
}
