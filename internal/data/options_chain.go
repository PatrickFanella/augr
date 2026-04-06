package data

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// OptionsProviderChain wraps multiple OptionsDataProviders and tries them in
// order, falling through on errors.
type OptionsProviderChain struct {
	providers []OptionsDataProvider
	logger    *slog.Logger
}

// NewOptionsProviderChain constructs a chain from an ordered list of providers.
func NewOptionsProviderChain(logger *slog.Logger, providers ...OptionsDataProvider) *OptionsProviderChain {
	if logger == nil {
		logger = slog.Default()
	}
	return &OptionsProviderChain{providers: providers, logger: logger}
}

func (c *OptionsProviderChain) GetOptionsChain(ctx context.Context, underlying string, expiry time.Time, optionType domain.OptionType) ([]domain.OptionSnapshot, error) {
	var lastErr error
	for _, p := range c.providers {
		result, err := p.GetOptionsChain(ctx, underlying, expiry, optionType)
		if err == nil {
			return result, nil
		}
		if errors.Is(err, ErrNotImplemented) {
			continue
		}
		c.logger.Warn("options chain provider failed, trying next",
			slog.String("underlying", underlying),
			slog.Any("error", err),
		)
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("options: no provider available for chain data")
}

func (c *OptionsProviderChain) GetOptionsOHLCV(ctx context.Context, occSymbol string, timeframe Timeframe, from, to time.Time) ([]domain.OHLCV, error) {
	var lastErr error
	for _, p := range c.providers {
		result, err := p.GetOptionsOHLCV(ctx, occSymbol, timeframe, from, to)
		if err == nil {
			return result, nil
		}
		if errors.Is(err, ErrNotImplemented) {
			continue
		}
		c.logger.Warn("options OHLCV provider failed, trying next",
			slog.String("symbol", occSymbol),
			slog.Any("error", err),
		)
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("options: no provider available for OHLCV data")
}
