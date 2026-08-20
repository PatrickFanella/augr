package capacity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/capital"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

type TierOutcome struct {
	Ordinal           int    `json:"ordinal"`
	Tier              string `json:"tier"`
	Viable            bool   `json:"viable"`
	Reason            string `json:"reason"`
	Units             int    `json:"units"`
	ExecutableCapital string `json:"executable_capital"`
	UnusedCapital     string `json:"unused_capital"`
	Saturated         bool   `json:"saturated"`
}
type FamilyOutcome struct {
	Family                 FamilyKind    `json:"family"`
	ContractID             string        `json:"contract_id"`
	ContractSHA256         string        `json:"contract_sha256"`
	AfterCostReturn        string        `json:"after_cost_return"`
	MinimumViableTier      string        `json:"minimum_viable_tier"`
	MinimumViableAvailable bool          `json:"minimum_viable_available"`
	Tiers                  []TierOutcome `json:"tiers"`
}
type (
	comparisonCanonical struct {
		Schema               string          `json:"schema"`
		State                string          `json:"state"`
		CapitalPolicyVersion string          `json:"capital_policy_version"`
		Families             []FamilyOutcome `json:"families"`
	}
	Comparison struct {
		canonical comparisonCanonical
		bytes     json.RawMessage
		digest    string
		id        uuid.UUID
	}
)

func NewComparison(policy *capital.Policy, contracts []*Contract) (*Comparison, error) {
	if policy == nil || len(contracts) != 5 {
		return nil, fmt.Errorf("capacity comparison requires policy and five families")
	}
	values := append([]*Contract(nil), contracts...)
	sort.Slice(values, func(i, j int) bool { return values[i].canonical.Family < values[j].canonical.Family })
	want := []FamilyKind{FamilyDefinedRisk, FamilyTrend, FamilyMomentum, FamilyPassive, FamilyWheel}
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	families := make([]FamilyOutcome, 5)
	tiers := policy.Tiers()
	for i, c := range values {
		if c == nil || c.canonical.Family != want[i] || i > 0 && values[i-1].canonical.Family == c.canonical.Family {
			return nil, fmt.Errorf("capacity comparison family set is invalid")
		}
		family := FamilyOutcome{Family: c.canonical.Family, ContractID: c.ID().String(), ContractSHA256: c.Digest(), AfterCostReturn: c.canonical.AfterCostReturn, Tiers: make([]TierOutcome, len(tiers))}
		for ordinal, tier := range tiers {
			outcome := TierOutcome{Ordinal: ordinal, Tier: tier.String(), Reason: c.canonical.UnavailableReason, ExecutableCapital: "0", UnusedCapital: tier.String()}
			if c.canonical.CapacityAvailable {
				per := decimal.RequireFromString(c.canonical.CapitalPerUnit)
				units := int(tier.Div(per).Floor().IntPart())
				if units > c.canonical.MaximumUnits {
					units = c.canonical.MaximumUnits
					outcome.Saturated = true
				}
				if units < 1 {
					outcome.Reason = "below_minimum_whole_unit"
				} else {
					outcome.Viable = true
					outcome.Reason = "admitted"
					outcome.Units = units
					used := per.Mul(decimal.NewFromInt(int64(units)))
					outcome.ExecutableCapital = used.String()
					outcome.UnusedCapital = tier.Sub(used).String()
					if !family.MinimumViableAvailable {
						family.MinimumViableAvailable = true
						family.MinimumViableTier = tier.String()
					}
				}
			}
			family.Tiers[ordinal] = outcome
		}
		families[i] = family
	}
	canonical := comparisonCanonical{ComparisonSchemaV1, "completed", policy.Version(), families}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Comparison{canonical, encoded, digest, economicid.DeterministicUUID("capital-tier-comparison", ComparisonSchemaV1+"@sha256:"+digest)}, nil
}

func ComparisonFromCanonical(id uuid.UUID, digest string, raw []byte, policy *capital.Policy, contracts []*Contract) (*Comparison, error) {
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hash(raw) != digest {
		return nil, fmt.Errorf("capacity comparison envelope is invalid")
	}
	value, err := NewComparison(policy, contracts)
	if err != nil || value.id != id || value.digest != digest || !bytes.Equal(value.bytes, raw) {
		return nil, fmt.Errorf("capacity comparison identity does not reconstruct")
	}
	return value, nil
}

func (c *Comparison) ID() uuid.UUID {
	if c == nil {
		return uuid.Nil
	}
	return c.id
}

func (c *Comparison) Digest() string {
	if c == nil {
		return ""
	}
	return c.digest
}

func (c *Comparison) CanonicalBytes() json.RawMessage {
	if c == nil {
		return nil
	}
	return append(json.RawMessage(nil), c.bytes...)
}

func (c *Comparison) Families() []FamilyOutcome {
	if c == nil {
		return nil
	}
	encoded, _ := json.Marshal(c.canonical.Families)
	var out []FamilyOutcome
	_ = json.Unmarshal(encoded, &out)
	return out
}
