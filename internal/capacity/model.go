// Package capacity owns deterministic capital-tier comparison evidence. It
// does not rank, promote, allocate, schedule, deploy, or execute strategies.
package capacity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/benchmark"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/evaluation"
	"github.com/PatrickFanella/get-rich-quick/internal/strategy/definedrisk"
	"github.com/PatrickFanella/get-rich-quick/internal/strategy/momentum"
	"github.com/PatrickFanella/get-rich-quick/internal/strategy/trend"
	"github.com/PatrickFanella/get-rich-quick/internal/strategy/wheel"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

const (
	ContractSchemaV1   = "family-capacity-contract-v1"
	ComparisonSchemaV1 = "capital-tier-comparison-v1"
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type FamilyKind string

const (
	FamilyPassive     FamilyKind = "passive_control"
	FamilyWheel       FamilyKind = "quality_filtered_wheel"
	FamilyMomentum    FamilyKind = "momentum_quality"
	FamilyTrend       FamilyKind = "etf_time_series_trend"
	FamilyDefinedRisk FamilyKind = "defined_risk_options"
)

type EvaluationEvidence interface {
	ID() uuid.UUID
	ProgramID() uuid.UUID
	Digest() string
	CanonicalBytes() json.RawMessage
	Mode() strategycatalog.ExperimentMode
	EvaluationStart() time.Time
	EvaluationEnd() time.Time
	Metrics() []evaluation.Metric
}

type contractCanonical struct {
	Schema             string     `json:"schema"`
	State              string     `json:"state"`
	Family             FamilyKind `json:"family"`
	EvaluationID       string     `json:"evaluation_id"`
	EvaluationSHA256   string     `json:"evaluation_sha256"`
	SourceReportID     string     `json:"source_report_id"`
	SourceReportSHA256 string     `json:"source_report_sha256"`
	EvaluationStart    string     `json:"evaluation_start"`
	EvaluationEnd      string     `json:"evaluation_end"`
	AfterCostReturn    string     `json:"after_cost_return"`
	CapacityAvailable  bool       `json:"capacity_available"`
	UnavailableReason  string     `json:"unavailable_reason"`
	CapitalPerUnit     string     `json:"capital_per_unit"`
	MaximumUnits       int        `json:"maximum_units"`
}
type Contract struct {
	canonical contractCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func FromBenchmark(e EvaluationEvidence, source *benchmark.Report) (*Contract, error) {
	if source == nil || e == nil || source.EvaluationID() != e.ID() {
		return nil, fmt.Errorf("capacity benchmark source is required")
	}
	return unavailable(e, FamilyPassive, source.ID(), source.Digest(), source.StrategyTotalReturn(), "source_capacity_not_observed")
}

func FromWheel(e EvaluationEvidence, program *wheel.Program) (*Contract, error) {
	if program == nil || program.Identity() == nil || program.Report() == nil || e == nil || e.ProgramID() != program.Identity().ID() {
		return nil, fmt.Errorf("capacity wheel source is required")
	}
	source := program.Report()
	return unavailable(e, FamilyWheel, source.ID(), source.Digest(), source.AfterCostTotalReturn(), "source_capacity_not_observed")
}

func FromMomentum(e EvaluationEvidence, program *momentum.Program) (*Contract, error) {
	if program == nil || program.Identity() == nil || program.Report() == nil || e == nil || e.ProgramID() != program.Identity().ID() {
		return nil, fmt.Errorf("capacity momentum source is required")
	}
	source := program.Report()
	return unavailable(e, FamilyMomentum, source.ID(), source.Digest(), source.AfterCostTotalReturn(), "source_capacity_not_observed")
}

func FromTrend(e EvaluationEvidence, program *trend.Program) (*Contract, error) {
	if program == nil || program.Identity() == nil || program.Report() == nil || e == nil || e.ProgramID() != program.Identity().ID() {
		return nil, fmt.Errorf("capacity trend source is required")
	}
	source := program.Report()
	return unavailable(e, FamilyTrend, source.ID(), source.Digest(), source.AfterCostTotalReturn(), "source_capacity_not_observed")
}

func FromDefinedRisk(e EvaluationEvidence, scenario *definedrisk.Scenario, program *definedrisk.Program) (*Contract, error) {
	if scenario == nil || program == nil || program.Identity() == nil || program.Report() == nil || e == nil || e.ProgramID() != program.Identity().ID() || program.Report().Outcome() != "settled" || program.Report().Contracts() < 1 {
		return nil, fmt.Errorf("capacity defined-risk source is incomplete")
	}
	source := program.Report()
	var report struct {
		AfterCostReturn string `json:"after_cost_total_return"`
	}
	var declared struct {
		Legs []struct {
			Position string `json:"position"`
			Entry    struct {
				BidSize string `json:"bid_size"`
				AskSize string `json:"ask_size"`
			} `json:"entry"`
		} `json:"legs"`
	}
	if json.Unmarshal(source.CanonicalBytes(), &report) != nil || json.Unmarshal(scenario.CanonicalBytes(), &declared) != nil || len(declared.Legs) != 2 {
		return nil, fmt.Errorf("capacity defined-risk source does not reconstruct")
	}
	maximum := int64(^uint64(0) >> 1)
	for _, leg := range declared.Legs {
		depth := leg.Entry.AskSize
		if leg.Position == "short" {
			depth = leg.Entry.BidSize
		}
		value, err := decimal.NewFromString(depth)
		if err != nil || value.IsNegative() {
			return nil, fmt.Errorf("capacity defined-risk depth is invalid")
		}
		units := value.Floor().IntPart()
		if units < maximum {
			maximum = units
		}
	}
	perUnit := decimal.RequireFromString(source.ReservedCapital()).Div(decimal.NewFromInt(int64(source.Contracts())))
	return newContract(e, FamilyDefinedRisk, source.ID(), source.Digest(), report.AfterCostReturn, true, "", perUnit.String(), int(maximum))
}

func unavailable(e EvaluationEvidence, f FamilyKind, id uuid.UUID, digest, ret, reason string) (*Contract, error) {
	return newContract(e, f, id, digest, ret, false, reason, "0", 0)
}

func newContract(e EvaluationEvidence, f FamilyKind, sourceID uuid.UUID, sourceDigest, sourceReturn string, available bool, reason, capitalPerUnit string, maxUnits int) (*Contract, error) {
	if e == nil || e.ID() == uuid.Nil || sourceID == uuid.Nil || !digestPattern.MatchString(e.Digest()) || !digestPattern.MatchString(sourceDigest) || hash(e.CanonicalBytes()) != e.Digest() || e.Mode() != strategycatalog.ExperimentPaperScored || !validDecimal(sourceReturn) {
		return nil, fmt.Errorf("capacity family evidence is invalid")
	}
	returnValue, ok := evaluationReturn(e.Metrics())
	if !ok || returnValue != sourceReturn {
		return nil, fmt.Errorf("capacity evaluation and source return do not agree")
	}
	if available {
		if !positive(capitalPerUnit) || maxUnits < 1 || reason != "" {
			return nil, fmt.Errorf("capacity available contract is invalid")
		}
	} else if capitalPerUnit != "0" || maxUnits != 0 || reason != "source_capacity_not_observed" {
		return nil, fmt.Errorf("capacity unavailable contract is invalid")
	}
	c := contractCanonical{ContractSchemaV1, "completed", f, e.ID().String(), e.Digest(), sourceID.String(), sourceDigest, formatTime(e.EvaluationStart()), formatTime(e.EvaluationEnd()), sourceReturn, available, reason, capitalPerUnit, maxUnits}
	encoded, _ := json.Marshal(c)
	digest := hash(encoded)
	return &Contract{c, encoded, digest, economicid.DeterministicUUID("family-capacity-contract", ContractSchemaV1+"@sha256:"+digest)}, nil
}

func evaluationReturn(values []evaluation.Metric) (string, bool) {
	for _, v := range values {
		if v.Section == "portfolio" && v.Name == "after_cost_total_return" && v.State == evaluation.MetricAvailable {
			return v.Value, true
		}
	}
	return "", false
}

func (c *Contract) ID() uuid.UUID {
	if c == nil {
		return uuid.Nil
	}
	return c.id
}

func (c *Contract) Digest() string {
	if c == nil {
		return ""
	}
	return c.digest
}

func (c *Contract) CanonicalBytes() json.RawMessage {
	if c == nil {
		return nil
	}
	return append(json.RawMessage(nil), c.bytes...)
}

func ContractFromCanonical(id uuid.UUID, digest string, raw []byte) (*Contract, error) {
	var c contractCanonical
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &c) != nil || c.Schema != ContractSchemaV1 || c.State != "completed" {
		return nil, fmt.Errorf("capacity contract envelope is invalid")
	}
	encoded, _ := json.Marshal(c)
	value := &Contract{c, encoded, digest, economicid.DeterministicUUID("family-capacity-contract", ContractSchemaV1+"@sha256:"+digest)}
	if value.id != id || !bytes.Equal(encoded, raw) {
		return nil, fmt.Errorf("capacity contract identity does not reconstruct")
	}
	return value, nil
}

func validDecimal(v string) bool {
	_, err := decimal.NewFromString(v)
	return err == nil && len(v) <= 128
}

func positive(v string) bool        { return validDecimal(v) && decimal.RequireFromString(v).IsPositive() }
func hash(v []byte) string          { s := sha256.Sum256(v); return hex.EncodeToString(s[:]) }
func formatTime(v time.Time) string { return v.Format("2006-01-02T15:04:05.000000Z") }
func decodeExact(raw []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("canonical JSON contains extra data")
	}
	return nil
}
