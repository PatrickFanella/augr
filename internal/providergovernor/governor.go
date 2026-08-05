package providergovernor

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type CooldownStore interface {
	GetProviderCooldown(context.Context, string) (time.Time, error)
	SetProviderCooldown(context.Context, string, time.Time) error
	CompareAndClearProviderCooldown(context.Context, string, time.Time) (bool, error)
}

type (
	Limiter interface{ Wait(context.Context) error }
	Sleeper interface {
		Sleep(context.Context, time.Duration) error
	}
)

type ProviderGovernor struct {
	Provider string
	Limiter  Limiter
	Cooldown CooldownStore
	Sleeper  Sleeper
	Clock    func() time.Time
	Rand     *rand.Rand
}

var ErrProviderCooldown = errors.New("provider cooldown active")

type RateLimitError struct {
	Provider   string
	ClientType string
	Method     string
	StatusCode int
	Body       string
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("%s %s %s rate limited: status=%d retry-after=%s", e.Provider, e.ClientType, e.Method, e.StatusCode, e.RetryAfter)
}

func ParseRetryAfter(v string, now func() time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if now == nil {
			now = time.Now
		}
		if d := t.Sub(now()); d > 0 {
			return d
		}
	}
	return 0
}

func (g *ProviderGovernor) Reserve(ctx context.Context) error {
	if g == nil || g.Limiter == nil {
		return nil
	}
	if g.Cooldown != nil {
		now := time.Now
		if g.Clock != nil {
			now = g.Clock
		}
		until, err := g.Cooldown.GetProviderCooldown(ctx, g.Provider)
		if err != nil {
			return err
		}
		if !until.IsZero() && now().Before(until) {
			return &RateLimitError{Provider: g.Provider, StatusCode: http.StatusTooManyRequests, RetryAfter: time.Until(until), Body: "provider cooldown active"}
		}
		if !until.IsZero() && !now().Before(until) {
			_, _ = g.Cooldown.CompareAndClearProviderCooldown(ctx, g.Provider, until)
		}
	}
	return g.Limiter.Wait(ctx)
}

func (g *ProviderGovernor) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	if g != nil && g.Sleeper != nil {
		return g.Sleeper.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func RetryAfter(err error) (time.Duration, bool) {
	var e *RateLimitError
	if !errors.As(err, &e) {
		return 0, false
	}
	return e.RetryAfter, e.RetryAfter > 0
}

func Jitter(base time.Duration, ratio float64, r *rand.Rand) time.Duration {
	if base <= 0 || ratio <= 0 {
		return base
	}
	if r == nil {
		r = rand.New(rand.NewSource(1))
	}
	offset := (r.Float64()*2 - 1) * ratio
	return time.Duration(float64(base) * (1 + offset))
}

func MaxAttempts(attempts int) int {
	if attempts < 1 {
		return 1
	}
	return attempts
}

func SleepContext(ctx context.Context, sleeper Sleeper, d time.Duration) error {
	if sleeper != nil {
		return sleeper.Sleep(ctx, d)
	}
	return (&ProviderGovernor{}).Sleep(ctx, d)
}

func NewContextSleeper() Sleeper { return contextSleeper{} }

type contextSleeper struct{}

func (contextSleeper) Sleep(ctx context.Context, d time.Duration) error {
	return (&ProviderGovernor{}).Sleep(ctx, d)
}

func Wrap(provider, clientType, method string, status int, retryAfter time.Duration, body string) error {
	return &RateLimitError{Provider: provider, ClientType: clientType, Method: method, StatusCode: status, RetryAfter: retryAfter, Body: body}
}
