package polymarket

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/prediction"
)

// Snapshot captures the native Polymarket execution state needed to decide
// whether a market can be activated safely.
type Snapshot struct {
	Slug               string
	Question           string
	Description        string
	ResolutionCriteria string
	ResolutionSource   string
	EndDate            *time.Time
	ConditionID        string
	YesTokenID         string
	NoTokenID          string
	YesPrice           float64
	NoPrice            float64
	BestBidYes         float64
	BestAskYes         float64
	SpreadYes          float64
	Liquidity          float64
	Volume24h          float64
	OpenInterest       float64
	BestBidNo          float64
	BestAskNo          float64
	FetchedAt          time.Time
}

// ValidateActivation checks whether the snapshot is executable for a native
// Polymarket strategy.
func (s Snapshot) ValidateActivation(minLiquidity float64, now time.Time) error {
	if err := s.validateExecutableBase(minLiquidity, now); err != nil {
		return err
	}
	switch {
	case strings.TrimSpace(s.YesTokenID) == "":
		return errors.New("polymarket snapshot: yes token id is required")
	case strings.TrimSpace(s.NoTokenID) == "":
		return errors.New("polymarket snapshot: no token id is required")
	}
	for _, side := range []string{"YES", "NO"} {
		bid, ask := s.BidAskForSide(side)
		if bid <= 0 || ask <= 0 || ask < bid || ask > 1 {
			return fmt.Errorf("polymarket snapshot: valid %s orderbook quote is required", side)
		}
	}
	return nil
}

// ValidateExecutableSide checks whether the snapshot has an executable book for
// the selected side.
func (s Snapshot) ValidateExecutableSide(side string, minLiquidity float64, now time.Time) error {
	if err := s.validateExecutableBase(minLiquidity, now); err != nil {
		return err
	}
	normalizedSide := prediction.NormalizeOutcomeSide(side)
	switch normalizedSide {
	case "YES", "NO":
	default:
		return errors.New("polymarket snapshot: side must be YES or NO")
	}
	bid, ask := s.BidAskForSide(normalizedSide)
	if bid <= 0 || ask <= 0 || ask < bid || ask > 1 {
		return fmt.Errorf("polymarket snapshot: valid %s orderbook quote is required", normalizedSide)
	}
	return nil
}

func (s Snapshot) validateExecutableBase(minLiquidity float64, now time.Time) error {
	switch {
	case strings.TrimSpace(s.Slug) == "":
		return errors.New("polymarket snapshot: slug is required")
	case s.EndDate == nil || !s.EndDate.After(now):
		return errors.New("polymarket snapshot: valid future end date is required")
	case s.Liquidity < minLiquidity:
		return fmt.Errorf("polymarket snapshot: liquidity %.2f below minimum %.2f", s.Liquidity, minLiquidity)
	}
	return nil
}

// SnapshotFromPredictionMarketData converts cached market data into a native
// execution snapshot.
func SnapshotFromPredictionMarketData(m *agent.PredictionMarketData, fetchedAt time.Time) Snapshot {
	if m == nil {
		return Snapshot{FetchedAt: fetchedAt}
	}
	return Snapshot{
		Slug:               m.Slug,
		Question:           m.Question,
		Description:        m.Description,
		ResolutionCriteria: m.ResolutionCriteria,
		ResolutionSource:   m.ResolutionSource,
		EndDate:            m.EndDate,
		ConditionID:        m.ConditionID,
		YesTokenID:         m.YesTokenID,
		NoTokenID:          m.NoTokenID,
		YesPrice:           m.YesPrice,
		NoPrice:            m.NoPrice,
		BestBidYes:         m.BestBidYes,
		BestAskYes:         m.BestAskYes,
		SpreadYes:          m.SpreadYes,
		Liquidity:          m.Liquidity,
		Volume24h:          m.Volume24h,
		OpenInterest:       m.OpenInterest,
		BestBidNo:          m.BestBidNo,
		BestAskNo:          m.BestAskNo,
		FetchedAt:          fetchedAt,
	}
}

// EntryPriceForSide returns the executable ask price for a YES or NO buy.
func (s Snapshot) EntryPriceForSide(side string) (float64, bool) {
	switch prediction.NormalizeOutcomeSide(side) {
	case "YES":
		return s.BestAskYes, s.BestAskYes > 0
	case "NO":
		return s.BestAskNo, s.BestAskNo > 0
	default:
		return 0, false
	}
}

// BidAskForSide returns the best bid and ask for the selected side.
func (s Snapshot) BidAskForSide(side string) (bid, ask float64) {
	switch prediction.NormalizeOutcomeSide(side) {
	case "YES":
		return s.BestBidYes, s.BestAskYes
	case "NO":
		return s.BestBidNo, s.BestAskNo
	default:
		return 0, 0
	}
}

// SpreadForSide returns the bid/ask spread for the selected side when known.
func (s Snapshot) SpreadForSide(side string) (float64, bool) {
	switch prediction.NormalizeOutcomeSide(side) {
	case "YES":
		if s.SpreadYes > 0 {
			return s.SpreadYes, true
		}
		if s.BestAskYes > 0 {
			return max0(s.BestAskYes - s.BestBidYes), true
		}
	case "NO":
		if s.BestBidNo > 0 && s.BestAskNo > 0 {
			return max0(s.BestAskNo - s.BestBidNo), true
		}
		if s.BestBidYes > 0 && s.BestAskYes > 0 {
			return max0(s.BestAskYes - s.BestBidYes), true
		}
	}
	return 0, false
}
