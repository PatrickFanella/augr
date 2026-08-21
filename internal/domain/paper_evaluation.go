package domain

import (
	"fmt"
	"strings"
)

// PaperEvaluationMode separates promotion-quality paper evidence from synthetic
// stress and chaos tests. Results from different modes must never share a
// storage namespace or ranking population.
type PaperEvaluationMode string

const (
	PaperEvaluationModeScored PaperEvaluationMode = "paper_scored"
	PaperEvaluationModeStress PaperEvaluationMode = "paper_stress"
)

const (
	PaperEvidenceClassPromotion = "promotion_evidence"
	PaperEvidenceClassSynthetic = "synthetic_stress"
)

// PaperEvaluationProfile is the immutable identity and economic input contract
// for one paper account. Margin behavior is deliberately deferred to the
// account/ledger work; BuyingPowerMultiplier records the requested policy.
type PaperEvaluationProfile struct {
	Mode                  PaperEvaluationMode `json:"mode"`
	StorageNamespace      string              `json:"storage_namespace"`
	EvidenceClass         string              `json:"evidence_class"`
	InitialCapital        float64             `json:"initial_capital"`
	BuyingPowerMultiplier float64             `json:"buying_power_multiplier"`
	SlippageBPS           float64             `json:"slippage_bps"`
	FeePct                float64             `json:"fee_pct"`
}

// NewPaperEvaluationProfile validates and materializes a mode-specific profile.
func NewPaperEvaluationProfile(mode PaperEvaluationMode, initialCapital, buyingPowerMultiplier, slippageBPS, feePct float64) (PaperEvaluationProfile, error) {
	mode = PaperEvaluationMode(strings.ToLower(strings.TrimSpace(string(mode))))
	if !mode.IsValid() {
		return PaperEvaluationProfile{}, fmt.Errorf("invalid paper evaluation mode %q", mode)
	}
	if initialCapital <= 0 {
		return PaperEvaluationProfile{}, fmt.Errorf("paper initial capital must be greater than zero")
	}
	if buyingPowerMultiplier < 0 {
		return PaperEvaluationProfile{}, fmt.Errorf("paper buying-power multiplier must be non-negative")
	}
	if mode == PaperEvaluationModeScored && buyingPowerMultiplier <= 0 {
		return PaperEvaluationProfile{}, fmt.Errorf("scored paper buying-power multiplier must be greater than zero")
	}
	if slippageBPS < 0 {
		return PaperEvaluationProfile{}, fmt.Errorf("paper slippage must be non-negative")
	}
	if feePct < 0 || feePct > 1 {
		return PaperEvaluationProfile{}, fmt.Errorf("paper fee percentage must be between zero and one")
	}

	evidenceClass := PaperEvidenceClassSynthetic
	if mode == PaperEvaluationModeScored {
		evidenceClass = PaperEvidenceClassPromotion
	}
	return PaperEvaluationProfile{
		Mode:                  mode,
		StorageNamespace:      string(mode),
		EvidenceClass:         evidenceClass,
		InitialCapital:        initialCapital,
		BuyingPowerMultiplier: buyingPowerMultiplier,
		SlippageBPS:           slippageBPS,
		FeePct:                feePct,
	}, nil
}

func (m PaperEvaluationMode) IsValid() bool {
	return m == PaperEvaluationModeScored || m == PaperEvaluationModeStress
}

// PromotionEligible is true only for broker-realistic scored evaluations.
func (p PaperEvaluationProfile) PromotionEligible() bool {
	return p.Mode == PaperEvaluationModeScored && p.EvidenceClass == PaperEvidenceClassPromotion
}

// CanShareStorageWith encodes the hard namespace boundary used by future
// account, ledger, metrics, and UI persistence.
func (p PaperEvaluationProfile) CanShareStorageWith(other PaperEvaluationProfile) bool {
	return p.StorageNamespace != "" && p.StorageNamespace == other.StorageNamespace && p.Mode == other.Mode
}
