package data_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// discardLogger returns a logger that discards all output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// stubProvider is a test double for DataProvider that returns fixed results or errors.
type stubProvider struct {
	ohlcv     []domain.OHLCV
	ohlcvErr  error
	fund      data.Fundamentals
	fundErr   error
	news      []data.NewsArticle
	newsErr   error
	sentiment []data.SocialSentiment
	sentErr   error
}

func (s *stubProvider) GetOHLCV(_ context.Context, _ string, _ data.Timeframe, _, _ time.Time) ([]domain.OHLCV, error) {
	return s.ohlcv, s.ohlcvErr
}

func (s *stubProvider) GetFundamentals(_ context.Context, _ string) (data.Fundamentals, error) {
	return s.fund, s.fundErr
}

func (s *stubProvider) GetNews(_ context.Context, _ string, _, _ time.Time) ([]data.NewsArticle, error) {
	return s.news, s.newsErr
}

func (s *stubProvider) GetSocialSentiment(_ context.Context, _ string, _, _ time.Time) ([]data.SocialSentiment, error) {
	return s.sentiment, s.sentErr
}

var errProviderFailed = errors.New("provider failed")

func TestProviderChainGetOHLCVFirstProviderSucceeds(t *testing.T) {
	want := []domain.OHLCV{{Open: 1, High: 2, Low: 0.5, Close: 1.5, Volume: 100}}
	chain := data.NewProviderChain(
		discardLogger(),
		&stubProvider{ohlcv: want},
		&stubProvider{ohlcvErr: errProviderFailed},
	)

	got, err := chain.GetOHLCV(context.Background(), "AAPL", data.Timeframe1d, time.Now(), time.Now())
	if err != nil {
		t.Fatalf("GetOHLCV() error = %v", err)
	}
	if len(got) != len(want) || got[0].Open != want[0].Open {
		t.Fatalf("GetOHLCV() = %v, want %v", got, want)
	}
}

func TestProviderChainGetOHLCVFallsBackOnFailure(t *testing.T) {
	want := []domain.OHLCV{{Open: 5, High: 6, Low: 4, Close: 5.5, Volume: 200}}
	chain := data.NewProviderChain(
		discardLogger(),
		&stubProvider{ohlcvErr: errProviderFailed},
		&stubProvider{ohlcv: want},
	)

	got, err := chain.GetOHLCV(context.Background(), "AAPL", data.Timeframe1h, time.Now(), time.Now())
	if err != nil {
		t.Fatalf("GetOHLCV() error = %v, want nil", err)
	}
	if len(got) != len(want) || got[0].Close != want[0].Close {
		t.Fatalf("GetOHLCV() = %v, want %v", got, want)
	}
}

func TestProviderChainGetOHLCVFallsBackOnEmpty(t *testing.T) {
	want := []domain.OHLCV{{Open: 5, High: 6, Low: 4, Close: 5.5, Volume: 200}}
	chain := data.NewProviderChain(
		discardLogger(),
		&stubProvider{ohlcv: []domain.OHLCV{}},
		&stubProvider{ohlcv: want},
	)

	got, err := chain.GetOHLCV(context.Background(), "AAPL", data.Timeframe5m, time.Now(), time.Now())
	if err != nil {
		t.Fatalf("GetOHLCV() error = %v, want nil", err)
	}
	if len(got) != len(want) || got[0].Close != want[0].Close {
		t.Fatalf("GetOHLCV() = %v, want fallback %v", got, want)
	}
}

func TestProviderChainGetOHLCVAllFail(t *testing.T) {
	chain := data.NewProviderChain(
		discardLogger(),
		&stubProvider{ohlcvErr: errProviderFailed},
		&stubProvider{ohlcvErr: errProviderFailed},
	)

	_, err := chain.GetOHLCV(context.Background(), "AAPL", data.Timeframe5m, time.Now(), time.Now())
	if !errors.Is(err, errProviderFailed) {
		t.Fatalf("GetOHLCV() error = %v, want %v", err, errProviderFailed)
	}
}

func TestProviderChainGetOHLCVDoesNotMaskFailureWithEmptyResult(t *testing.T) {
	chain := data.NewProviderChain(
		discardLogger(),
		&stubProvider{ohlcv: []domain.OHLCV{}},
		&stubProvider{ohlcvErr: errProviderFailed},
		&stubProvider{ohlcvErr: data.ErrNotImplemented},
	)

	_, err := chain.GetOHLCV(context.Background(), "AAPL", data.Timeframe5m, time.Now(), time.Now())
	if !errors.Is(err, data.ErrProviderCoverageIncomplete) {
		t.Fatalf("GetOHLCV() error = %v, want ErrProviderCoverageIncomplete", err)
	}
	if !errors.Is(err, errProviderFailed) {
		t.Fatalf("GetOHLCV() error = %v, want wrapped provider failure", err)
	}
}

func TestProviderChainGetOHLCVNoProviders(t *testing.T) {
	chain := data.NewProviderChain(discardLogger())

	_, err := chain.GetOHLCV(context.Background(), "AAPL", data.Timeframe1m, time.Now(), time.Now())
	if !errors.Is(err, data.ErrNoProviders) {
		t.Fatalf("GetOHLCV() error = %v, want ErrNoProviders", err)
	}
}

func TestProviderChainGetFundamentalsFallback(t *testing.T) {
	want := data.Fundamentals{Ticker: "GOOG", PERatio: 25.5}
	chain := data.NewProviderChain(
		discardLogger(),
		&stubProvider{fundErr: errProviderFailed},
		&stubProvider{fund: want},
	)

	got, err := chain.GetFundamentals(context.Background(), "GOOG")
	if err != nil {
		t.Fatalf("GetFundamentals() error = %v", err)
	}
	if got.Ticker != want.Ticker || got.PERatio != want.PERatio {
		t.Fatalf("GetFundamentals() = %v, want %v", got, want)
	}
}

func TestProviderChainGetFundamentalsBackfillsMissingFields(t *testing.T) {
	first := data.Fundamentals{
		Ticker:        "AAPL",
		PERatio:       80.1,
		DividendYield: 0,
		MissingFields: data.MissingFundamentalFields(data.FundamentalFieldEPS, data.FundamentalFieldFreeCashFlow),
	}
	second := data.Fundamentals{
		Ticker:        "AAPL",
		EPS:           6.12,
		FreeCashFlow:  100,
		MissingFields: data.MissingFundamentalFields(data.FundamentalFieldDividendYield),
	}
	chain := data.NewProviderChain(
		discardLogger(),
		&stubProvider{fund: first},
		&stubProvider{fund: second},
	)

	got, err := chain.GetFundamentals(context.Background(), "AAPL")
	if err != nil {
		t.Fatalf("GetFundamentals() error = %v", err)
	}
	if got.PERatio != 80.1 || got.EPS != 6.12 || got.FreeCashFlow != 100 {
		t.Fatalf("GetFundamentals() = %#v, want P/E from first and EPS/FCF from second", got)
	}
	if got.DividendYield != 0 {
		t.Fatalf("DividendYield = %v, want real zero from first provider preserved", got.DividendYield)
	}
	if len(got.MissingFields) != 0 {
		t.Fatalf("MissingFields = %v, want none", got.MissingFields)
	}
}

func TestProviderChainGetFundamentalsNoProviders(t *testing.T) {
	chain := data.NewProviderChain(discardLogger())

	_, err := chain.GetFundamentals(context.Background(), "GOOG")
	if !errors.Is(err, data.ErrNoProviders) {
		t.Fatalf("GetFundamentals() error = %v, want ErrNoProviders", err)
	}
}

func TestProviderChainGetNewsFallback(t *testing.T) {
	want := []data.NewsArticle{{Title: "Rally!", Source: "Reuters"}}
	chain := data.NewProviderChain(
		discardLogger(),
		&stubProvider{newsErr: errProviderFailed},
		&stubProvider{news: want},
	)

	got, err := chain.GetNews(context.Background(), "TSLA", time.Now(), time.Now())
	if err != nil {
		t.Fatalf("GetNews() error = %v", err)
	}
	if len(got) != 1 || got[0].Title != want[0].Title {
		t.Fatalf("GetNews() = %v, want %v", got, want)
	}
}

func TestProviderChainGetNewsNoProviders(t *testing.T) {
	chain := data.NewProviderChain(discardLogger())

	_, err := chain.GetNews(context.Background(), "TSLA", time.Now(), time.Now())
	if !errors.Is(err, data.ErrNoProviders) {
		t.Fatalf("GetNews() error = %v, want ErrNoProviders", err)
	}
}

func TestProviderChainGetSocialSentimentFallback(t *testing.T) {
	want := []data.SocialSentiment{{Ticker: "BTC", Score: 0.8, MeasuredAt: time.Now()}}
	chain := data.NewProviderChain(
		discardLogger(),
		&stubProvider{sentErr: errProviderFailed},
		&stubProvider{sentiment: want},
	)

	got, err := chain.GetSocialSentiment(context.Background(), "BTC", time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatalf("GetSocialSentiment() error = %v", err)
	}
	if len(got) != len(want) || got[0].Ticker != want[0].Ticker || got[0].Score != want[0].Score {
		t.Fatalf("GetSocialSentiment() = %v, want %v", got, want)
	}
}

func TestProviderChainGetSocialSentimentNoProviders(t *testing.T) {
	chain := data.NewProviderChain(discardLogger())

	_, err := chain.GetSocialSentiment(context.Background(), "BTC", time.Now().Add(-time.Hour), time.Now())
	if !errors.Is(err, data.ErrNoProviders) {
		t.Fatalf("GetSocialSentiment() error = %v, want ErrNoProviders", err)
	}
}

func TestProviderChainNilLoggerDoesNotPanic(t *testing.T) {
	// Passing nil logger must not panic when a provider fails and the chain logs.
	chain := data.NewProviderChain(
		nil,
		&stubProvider{ohlcvErr: errProviderFailed},
		&stubProvider{ohlcv: []domain.OHLCV{{Open: 1}}},
	)

	got, err := chain.GetOHLCV(context.Background(), "AAPL", data.Timeframe1d, time.Now(), time.Now())
	if err != nil {
		t.Fatalf("GetOHLCV() with nil logger error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetOHLCV() with nil logger len = %d, want 1", len(got))
	}
}

func TestTimeframeString(t *testing.T) {
	tests := []struct {
		tf   data.Timeframe
		want string
	}{
		{data.Timeframe1m, "1m"},
		{data.Timeframe5m, "5m"},
		{data.Timeframe15m, "15m"},
		{data.Timeframe1h, "1h"},
		{data.Timeframe1d, "1d"},
	}

	for _, tc := range tests {
		if got := tc.tf.String(); got != tc.want {
			t.Errorf("Timeframe(%q).String() = %q, want %q", tc.tf, got, tc.want)
		}
	}
}

// Compile-time assertion: ProviderChain must satisfy DataProvider.
var _ data.DataProvider = (*data.ProviderChain)(nil)
