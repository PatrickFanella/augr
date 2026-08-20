package venuerecon

import (
	"context"
	"errors"
	"fmt"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
)

// NormalizeKalshiCapture is the pure Kalshi boundary. It deliberately exposes
// no authenticated transport or provider mutation capability.
func NormalizeKalshiCapture(ctx context.Context, input CaptureInput, resolver ContractResolver) (*ProviderCapture, error) {
	if input.Provider != venue.ProviderKalshi {
		return nil, fmt.Errorf("kalshi capture has provider %q", input.Provider)
	}
	derived, err := deriveCaptureFromRawPages(input)
	if err != nil {
		return nil, NewCaptureFailure(ReasonSnapshotIncomplete, fmt.Errorf("parse Kalshi capture pages: %w", err))
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
