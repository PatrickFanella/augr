package venuerecon

import (
	"context"
	"fmt"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
)

// NormalizeAlpacaCapture is the pure Alpaca boundary. Transport and credentials
// stay outside reconciliation; callers supply already captured wire facts and
// the exact raw page bytes from which they were parsed.
func NormalizeAlpacaCapture(ctx context.Context, input CaptureInput, resolver ContractResolver) (*ProviderCapture, error) {
	if input.Provider != venue.ProviderAlpaca {
		return nil, fmt.Errorf("alpaca capture has provider %q", input.Provider)
	}
	derived, err := deriveCaptureFromRawPages(input)
	if err != nil {
		return nil, fmt.Errorf("parse Alpaca capture pages: %w", err)
	}
	return newProviderCapture(ctx, derived, resolver)
}
