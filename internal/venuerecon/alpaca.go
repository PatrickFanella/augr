package venuerecon

import (
	"context"
	"errors"
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
		return nil, NewCaptureFailure(ReasonSnapshotIncomplete, fmt.Errorf("parse Alpaca capture pages: %w", err))
	}
	capture, err := newProviderCapture(ctx, derived, resolver)
	if err == nil {
		return capture, nil
	}
	var mapping *contractResolutionError
	if errors.As(err, &mapping) {
		return nil, NewCaptureFailure(ReasonSnapshotMappingFailure, err)
	}
	return nil, NewCaptureFailure(ReasonSnapshotIncomplete, err)
}
