package api

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CORSConfig holds settings for the CORS middleware.
type CORSConfig struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
	MaxAge         int // seconds
}

// DefaultCORSConfig returns a permissive CORS configuration suitable for local development.
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type"},
		MaxAge:         86400,
	}
}

// CORS returns middleware that sets Cross-Origin Resource Sharing headers.
// When configured with "*", the wildcard is returned. When specific origins are
// listed, the middleware echoes back the request Origin only when it matches
// one of the allowed values.
func CORS(cfg CORSConfig) func(http.Handler) http.Handler {
	methods := strings.Join(cfg.AllowedMethods, ", ")
	headers := strings.Join(cfg.AllowedHeaders, ", ")
	maxAgeStr := strconv.Itoa(cfg.MaxAge)

	allowAll := len(cfg.AllowedOrigins) == 1 && cfg.AllowedOrigins[0] == "*"
	allowed := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		allowed[o] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if allowAll {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if origin != "" {
				if _, ok := allowed[origin]; ok {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
				}
			}
			w.Header().Set("Access-Control-Allow-Methods", methods)
			w.Header().Set("Access-Control-Allow-Headers", headers)
			w.Header().Set("Access-Control-Max-Age", maxAgeStr)

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimiter provides simple per-client rate limiting based on a fixed
// window counter keyed by the client's IP address. Expired entries are
// evicted lazily during Allow() calls.
type RateLimiter struct {
	mu             sync.Mutex
	clients        map[string]*clientWindow
	limit          int
	window         time.Duration
	nowFunc        func() time.Time
	trustedProxies []*net.IPNet
}

type clientWindow struct {
	count    int
	resetAt  time.Time
}

// NewRateLimiter creates a rate limiter that allows limit requests per window
// per client IP.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		clients: make(map[string]*clientWindow),
		limit:   limit,
		window:  window,
		nowFunc: time.Now,
	}
}

// evictExpiredLocked removes entries whose window has expired. Must be called
// with rl.mu held.
func (rl *RateLimiter) evictExpiredLocked(now time.Time) {
	for key, cw := range rl.clients {
		if now.After(cw.resetAt) {
			delete(rl.clients, key)
		}
	}
}

// Allow returns true if the client identified by key has not exceeded the rate
// limit.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.nowFunc()

	// Periodically evict expired entries to bound map growth.
	if len(rl.clients) > rl.limit {
		rl.evictExpiredLocked(now)
	}

	cw, ok := rl.clients[key]
	if !ok || now.After(cw.resetAt) {
		rl.clients[key] = &clientWindow{count: 1, resetAt: now.Add(rl.window)}
		return true
	}
	cw.count++
	return cw.count <= rl.limit
}

// Middleware returns an http.Handler middleware that rejects requests exceeding
// the rate limit with 429 Too Many Requests.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := clientIP(r, rl.trustedProxies)
		if !rl.Allow(key) {
			respondError(w, http.StatusTooManyRequests, "rate limit exceeded", ErrCodeRateLimited)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP extracts the client IP from the request. It only trusts
// X-Forwarded-For and X-Real-IP headers when the immediate peer is
// within one of the configured trusted proxy CIDR ranges.
func clientIP(r *http.Request, trustedProxies []*net.IPNet) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	if len(trustedProxies) > 0 {
		peerIP := net.ParseIP(host)
		if isTrustedProxy(peerIP, trustedProxies) {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				if parts := strings.SplitN(xff, ",", 2); len(parts) > 0 {
					return strings.TrimSpace(parts[0])
				}
			}
			if xri := r.Header.Get("X-Real-IP"); xri != "" {
				return xri
			}
		}
	}

	return host
}

// isTrustedProxy reports whether ip falls within any of the given CIDR ranges.
func isTrustedProxy(ip net.IP, nets []*net.IPNet) bool {
	if ip == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ParseTrustedProxies parses a slice of CIDR strings into net.IPNet values.
func ParseTrustedProxies(cidrs []string) ([]*net.IPNet, error) {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		if cidr == "" {
			continue
		}
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy CIDR %q: %w", cidr, err)
		}
		nets = append(nets, ipnet)
	}
	return nets, nil
}

// RequestLogger returns middleware that logs each HTTP request using the
// provided structured logger.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusCapture{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(sw, r)

			logger.Info("http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", sw.status),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			)
		})
	}
}

// statusCapture wraps http.ResponseWriter to record the status code.
type statusCapture struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (sw *statusCapture) WriteHeader(code int) {
	if !sw.wroteHeader {
		sw.status = code
		sw.wroteHeader = true
		sw.ResponseWriter.WriteHeader(code)
	}
}

func (sw *statusCapture) Write(b []byte) (int, error) {
	if !sw.wroteHeader {
		sw.WriteHeader(http.StatusOK)
	}
	return sw.ResponseWriter.Write(b)
}
