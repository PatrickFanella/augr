package data

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

type optionsStubProvider struct {
	chain      []domain.OptionSnapshot
	chainErr   error
	chainCalls int
	ohlcv      []domain.OHLCV
	ohlcvErr   error
	ohlcvCalls int
}

func (s *optionsStubProvider) GetOptionsChain(_ context.Context, _ string, _ time.Time, _ domain.OptionType) ([]domain.OptionSnapshot, error) {
	s.chainCalls++
	return s.chain, s.chainErr
}

func (s *optionsStubProvider) GetOptionsOHLCV(_ context.Context, _ string, _ Timeframe, _, _ time.Time) ([]domain.OHLCV, error) {
	s.ohlcvCalls++
	return s.ohlcv, s.ohlcvErr
}

func providerNames(t *testing.T, providers []DataProvider) []string {
	t.Helper()

	names := make([]string, 0, len(providers))
	for _, p := range providers {
		stub, ok := p.(*serviceStubProvider)
		if !ok {
			t.Fatalf("provider type = %T, want *serviceStubProvider", p)
		}
		names = append(names, stub.name)
	}
	return names
}

func TestSelectionPolicyBuildProviderChainsAndCacheRouting(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	policy := SelectionPolicy{}
	reg := &ProviderRegistry{
		Polygon:      func(ProviderConfig) DataProvider { return &serviceStubProvider{name: "polygon"} },
		AlphaVantage: func(ProviderConfig) DataProvider { return &serviceStubProvider{name: "alpha"} },
		Finnhub:      func(ProviderConfig) DataProvider { return &serviceStubProvider{name: "finnhub"} },
		FMP:          func(ProviderConfig) DataProvider { return &serviceStubProvider{name: "fmp"} },
		NewsAPI:      func(ProviderConfig) DataProvider { return &serviceStubProvider{name: "newsapi"} },
		Yahoo:        func(ProviderConfig) DataProvider { return &serviceStubProvider{name: "yahoo"} },
		Binance:      func(ProviderConfig) DataProvider { return &serviceStubProvider{name: "binance"} },
		Polymarket:   func(ProviderConfig) DataProvider { return &serviceStubProvider{name: "polymarket"} },
		Kalshi:       func(ProviderConfig) DataProvider { return &serviceStubProvider{name: "kalshi"} },
		StockTwits:   func(ProviderConfig) DataProvider { return &serviceStubProvider{name: "stocktwits"} },
	}

	chains := policy.BuildProviderChains(config.Config{
		DataProviders: config.DataProviderConfigs{
			Polygon:      config.DataProviderConfig{APIKey: "polygon-key"},
			AlphaVantage: config.DataProviderConfig{APIKey: "alpha-key"},
			Finnhub:      config.DataProviderConfig{APIKey: "finnhub-key"},
			FMP:          config.DataProviderConfig{APIKey: "fmp-key"},
			NewsAPI:      config.DataProviderConfig{APIKey: "newsapi-key"},
		},
		Brokers: config.BrokerConfigs{Polymarket: config.PolymarketConfig{CLOBURL: "https://clob.example"}},
	}, reg, logger, nil)

	if got, want := providerNames(t, chains.Stock), []string{"yahoo", "polygon", "finnhub", "fmp", "alpha", "newsapi"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stock providers = %v, want %v", got, want)
	}
	if got, want := providerNames(t, chains.Crypto), []string{"binance"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("crypto providers = %v, want %v", got, want)
	}
	if got, want := providerNames(t, chains.Polymarket), []string{"polymarket"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("polymarket providers = %v, want %v", got, want)
	}
	if got, want := providerNames(t, chains.Kalshi), []string{"kalshi"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("kalshi providers = %v, want %v", got, want)
	}
	if got, want := providerNames(t, chains.Social), []string{"finnhub", "stocktwits"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("social providers = %v, want %v", got, want)
	}

	for _, tc := range []struct {
		name       string
		marketType domain.MarketType
		dataType   string
		wantProv   string
	}{
		{name: "stock ohlcv", marketType: domain.MarketTypeStock, dataType: cacheDataTypeOHLCV, wantProv: cacheProviderStockChain},
		{name: "crypto ohlcv", marketType: domain.MarketTypeCrypto, dataType: cacheDataTypeOHLCV, wantProv: cacheProviderCryptoChain},
		{name: "polymarket ohlcv", marketType: domain.MarketTypePolymarket, dataType: cacheDataTypeOHLCV, wantProv: cacheProviderPolymarketChain},
		{name: "kalshi ohlcv", marketType: domain.MarketTypeKalshi, dataType: cacheDataTypeOHLCV, wantProv: cacheProviderKalshiChain},
		{name: "fundamentals", marketType: domain.MarketTypeStock, dataType: cacheDataTypeFundamentals, wantProv: cacheProviderStockChain},
		{name: "news", marketType: domain.MarketTypeStock, dataType: cacheDataTypeNews, wantProv: cacheProviderStockChain},
		{name: "social", marketType: domain.MarketTypeCrypto, dataType: cacheDataTypeSocial, wantProv: cacheProviderSocialAgg},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sel, err := policy.CacheSelection(tc.marketType, tc.dataType)
			if err != nil {
				t.Fatalf("CacheSelection() error = %v", err)
			}
			if !sel.Enabled {
				t.Fatalf("CacheSelection().Enabled = false, want true")
			}
			if sel.Provider != tc.wantProv {
				t.Fatalf("CacheSelection().Provider = %q, want %q", sel.Provider, tc.wantProv)
			}
		})
	}

	disabled, err := policy.CacheSelection(domain.MarketTypeStock, "unknown")
	if err != nil {
		t.Fatalf("CacheSelection(unknown) error = %v", err)
	}
	if disabled.Enabled {
		t.Fatalf("CacheSelection(unknown).Enabled = true, want false")
	}

	stock := &serviceStubProvider{name: "stock"}
	crypto := &serviceStubProvider{name: "crypto"}
	polymarket := &serviceStubProvider{name: "polymarket"}
	kalshi := &serviceStubProvider{name: "kalshi"}

	for _, tc := range []struct {
		name       string
		marketType domain.MarketType
		wantName   string
		wantChain  DataProvider
	}{
		{name: "stock", marketType: domain.MarketTypeStock, wantName: cacheProviderStockChain, wantChain: stock},
		{name: "crypto", marketType: domain.MarketTypeCrypto, wantName: cacheProviderCryptoChain, wantChain: crypto},
		{name: "polymarket", marketType: domain.MarketTypePolymarket, wantName: cacheProviderPolymarketChain, wantChain: polymarket},
		{name: "kalshi", marketType: domain.MarketTypeKalshi, wantName: cacheProviderKalshiChain, wantChain: kalshi},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotName, gotChain, err := policy.ResolveMarketChain(tc.marketType, stock, crypto, polymarket, kalshi)
			if err != nil {
				t.Fatalf("ResolveMarketChain() error = %v", err)
			}
			if gotName != tc.wantName {
				t.Fatalf("ResolveMarketChain() provider = %q, want %q", gotName, tc.wantName)
			}
			if gotChain != tc.wantChain {
				t.Fatalf("ResolveMarketChain() chain = %T, want %T", gotChain, tc.wantChain)
			}
		})
	}

	if _, _, err := policy.ResolveMarketChain("forex", stock, crypto, polymarket, kalshi); !errors.Is(err, ErrUnsupportedMarketType) {
		t.Fatalf("ResolveMarketChain() error = %v, want ErrUnsupportedMarketType", err)
	}
}

func TestOptionsProviderChainFallbackOrder(t *testing.T) {
	first := &optionsStubProvider{chainErr: ErrNotImplemented, ohlcvErr: ErrNotImplemented}
	second := &optionsStubProvider{chain: []domain.OptionSnapshot{{Contract: domain.OptionContract{OCCSymbol: "AAPL240119C00150000"}}}, ohlcv: []domain.OHLCV{{Close: 1}}}
	third := &optionsStubProvider{chainErr: errors.New("should not be reached"), ohlcvErr: errors.New("should not be reached")}
	chain := NewOptionsProviderChain(slog.New(slog.NewTextHandler(io.Discard, nil)), first, second, third)

	chainResult, err := chain.GetOptionsChain(context.Background(), "AAPL", time.Time{}, "")
	if err != nil {
		t.Fatalf("GetOptionsChain() error = %v", err)
	}
	if len(chainResult) != 1 || chainResult[0].Contract.OCCSymbol != "AAPL240119C00150000" {
		t.Fatalf("GetOptionsChain() = %#v, want second provider result", chainResult)
	}
	if first.chainCalls != 1 || second.chainCalls != 1 || third.chainCalls != 0 {
		t.Fatalf("chain calls = %d/%d/%d, want 1/1/0", first.chainCalls, second.chainCalls, third.chainCalls)
	}

	bars, err := chain.GetOptionsOHLCV(context.Background(), "AAPL240119C00150000", Timeframe1d, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("GetOptionsOHLCV() error = %v", err)
	}
	if len(bars) != 1 || bars[0].Close != 1 {
		t.Fatalf("GetOptionsOHLCV() = %#v, want second provider result", bars)
	}
	if first.ohlcvCalls != 1 || second.ohlcvCalls != 1 || third.ohlcvCalls != 0 {
		t.Fatalf("ohlcv calls = %d/%d/%d, want 1/1/0", first.ohlcvCalls, second.ohlcvCalls, third.ohlcvCalls)
	}
}

func TestOptionsProviderChainFallsThroughEmptyResults(t *testing.T) {
	first := &optionsStubProvider{}
	second := &optionsStubProvider{chain: []domain.OptionSnapshot{{Contract: domain.OptionContract{OCCSymbol: "AAPL240119C00150000"}}}}
	chain := NewOptionsProviderChain(slog.New(slog.NewTextHandler(io.Discard, nil)), first, second)

	result, err := chain.GetOptionsChain(context.Background(), "AAPL", time.Time{}, "")
	if err != nil {
		t.Fatalf("GetOptionsChain() error = %v", err)
	}
	if len(result) != 1 || result[0].Contract.OCCSymbol != "AAPL240119C00150000" {
		t.Fatalf("GetOptionsChain() = %#v, want second provider result", result)
	}
	if first.chainCalls != 1 || second.chainCalls != 1 {
		t.Fatalf("chain calls = %d/%d, want 1/1", first.chainCalls, second.chainCalls)
	}
}

func TestOptionsProviderChainErrorsWhenEveryProviderIsEmpty(t *testing.T) {
	first := &optionsStubProvider{}
	second := &optionsStubProvider{}
	chain := NewOptionsProviderChain(slog.New(slog.NewTextHandler(io.Discard, nil)), first, second)

	if _, err := chain.GetOptionsChain(context.Background(), "AAPL", time.Time{}, ""); err == nil {
		t.Fatal("GetOptionsChain() error = nil, want empty-chain error")
	}
	if first.chainCalls != 1 || second.chainCalls != 1 {
		t.Fatalf("chain calls = %d/%d, want 1/1", first.chainCalls, second.chainCalls)
	}
}
