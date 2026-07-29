package kalshi

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Snapshot captures the native Kalshi execution state needed to decide whether
// a market can be activated safely.
type Snapshot struct {
	Ticker       string
	Title        string
	Status       string
	BestBidYes   float64
	BestAskYes   float64
	BestBidNo    float64
	BestAskNo    float64
	Volume       float64
	OpenInterest float64
	CloseTime    time.Time
	FetchedAt    time.Time
}

// ValidateExecutableSide checks whether the snapshot has an executable book for
// the selected side.
func (s Snapshot) ValidateExecutableSide(side string, minLiquidity float64, now time.Time) error {
	if err := s.validateExecutableBase(minLiquidity, now); err != nil {
		return err
	}

	normalizedSide := strings.ToUpper(strings.TrimSpace(side))
	switch normalizedSide {
	case "YES", "NO":
	default:
		return errors.New("kalshi snapshot: side must be YES or NO")
	}

	bid, ask, ok := s.quoteForSide(normalizedSide)
	if !ok || bid <= 0 || ask <= 0 || ask < bid || ask > 1 {
		return fmt.Errorf("kalshi snapshot: valid %s orderbook quote is required", normalizedSide)
	}

	return nil
}

// EntryPriceForSide returns the executable ask price for a YES or NO buy.
func (s Snapshot) EntryPriceForSide(side string) (float64, bool) {
	_, ask, ok := s.quoteForSide(side)
	return ask, ok
}

// ExitPriceForSide returns the executable bid for selling a held contract.
func (s Snapshot) ExitPriceForSide(side string) (float64, bool) {
	bid, _, ok := s.quoteForSide(side)
	return bid, ok
}

// SpreadForSide returns the bid/ask spread for the selected side when known.
func (s Snapshot) SpreadForSide(side string) (float64, bool) {
	bid, ask, ok := s.quoteForSide(side)
	if !ok {
		return 0, false
	}
	return max0(ask - bid), true
}

func (s Snapshot) validateExecutableBase(minLiquidity float64, now time.Time) error {
	switch {
	case strings.TrimSpace(s.Ticker) == "":
		return errors.New("kalshi snapshot: ticker is required")
	case strings.TrimSpace(s.Title) == "":
		return errors.New("kalshi snapshot: title is required")
	case strings.TrimSpace(s.Status) == "":
		return errors.New("kalshi snapshot: status is required")
	}

	status := strings.ToLower(strings.TrimSpace(s.Status))
	switch status {
	case "closed", "settled", "expired":
		return fmt.Errorf("kalshi snapshot: market status %q is not executable", s.Status)
	}

	if s.CloseTime.IsZero() || !s.CloseTime.After(now) {
		return errors.New("kalshi snapshot: valid future close time is required")
	}

	if s.liquidityScore() < minLiquidity {
		return fmt.Errorf("kalshi snapshot: liquidity %.2f below minimum %.2f", s.liquidityScore(), minLiquidity)
	}

	return nil
}

func (s Snapshot) liquidityScore() float64 {
	if s.OpenInterest > s.Volume {
		return s.OpenInterest
	}
	return s.Volume
}

func (s Snapshot) quoteForSide(side string) (bid, ask float64, ok bool) {
	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "YES":
		return s.BestBidYes, s.BestAskYes, s.BestBidYes > 0 && s.BestAskYes > 0 && s.BestAskYes >= s.BestBidYes
	case "NO":
		return s.BestBidNo, s.BestAskNo, s.BestBidNo > 0 && s.BestAskNo > 0 && s.BestAskNo >= s.BestBidNo
	default:
		return 0, 0, false
	}
}

func max0(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}
