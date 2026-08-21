// Package copydrift owns deterministic, prepared-only multi-session copy
// target reconciliation evidence. It does not source positions or route orders.
package copydrift

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const (
	SchemaV1           = "copy-target-drift-session-v1"
	CalculationVersion = 1
)

var (
	digestPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	sessionPattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})/(regular|pre_market|after_hours)$`)
)

type Value struct {
	InstrumentKey string
	Amount        decimal.Decimal
}

type Input struct {
	Subscription        domain.CopySubscription
	SourceObservationID uuid.UUID
	SessionKey          string
	SessionBudget       decimal.Decimal
	Targets             []Value
	Currents            []Value
}

type valueCanonical struct {
	InstrumentKey string `json:"instrument_key"`
	Value         string `json:"value"`
}

type legCanonical struct {
	Sequence          int    `json:"sequence"`
	InstrumentKey     string `json:"instrument_key"`
	Side              string `json:"side"`
	CurrentValue      string `json:"current_value"`
	TargetValue       string `json:"target_value"`
	StartingDrift     string `json:"starting_drift"`
	RequestedNotional string `json:"requested_notional"`
	ProjectedValue    string `json:"projected_value"`
	ResidualDrift     string `json:"residual_drift"`
}

type runCanonical struct {
	Schema                 string           `json:"schema"`
	State                  string           `json:"state"`
	SubscriptionID         string           `json:"subscription_id"`
	OriginType             string           `json:"origin_type"`
	OriginID               string           `json:"origin_id"`
	SourceObservationID    string           `json:"source_observation_id"`
	SessionKey             string           `json:"session_key"`
	CalculationVersion     int              `json:"calculation_version"`
	MaximumSessionTurnover string           `json:"maximum_session_turnover"`
	SessionBudget          string           `json:"session_budget"`
	StartingDrift          string           `json:"starting_drift"`
	PreparedTurnover       string           `json:"prepared_turnover"`
	ResidualDrift          string           `json:"residual_drift"`
	Converged              bool             `json:"converged"`
	Targets                []valueCanonical `json:"targets"`
	Currents               []valueCanonical `json:"currents"`
	Legs                   []legCanonical   `json:"legs"`
}

type Run struct {
	canonical runCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewRun(input Input) (*Run, error) {
	sub := input.Subscription
	if sub.ID == uuid.Nil || sub.OriginType != "copy_subscription" || sub.OriginID != sub.ID || !sub.IsPaper || input.SourceObservationID == uuid.Nil {
		return nil, fmt.Errorf("copy drift requires exact paper subscription attribution")
	}
	if !validSession(input.SessionKey) {
		return nil, fmt.Errorf("copy drift session key is invalid")
	}
	maximum := decimal.NewFromFloat(sub.CapitalBudget).Mul(decimal.NewFromFloat(sub.MaxTurnoverPct)).Round(2)
	if !validMoney(input.SessionBudget) || input.SessionBudget.IsNegative() || !maximum.IsPositive() || input.SessionBudget.GreaterThan(maximum) {
		return nil, fmt.Errorf("copy drift session budget is invalid")
	}
	targets, targetMap, err := normalizeValues(input.Targets)
	if err != nil {
		return nil, fmt.Errorf("copy drift targets: %w", err)
	}
	currents, currentMap, err := normalizeValues(input.Currents)
	if err != nil {
		return nil, fmt.Errorf("copy drift currents: %w", err)
	}
	keys := unionKeys(targetMap, currentMap)
	starting := decimal.Zero
	for _, key := range keys {
		starting = starting.Add(targetMap[key].Sub(currentMap[key]).Abs())
	}
	if starting.IsPositive() && !input.SessionBudget.IsPositive() {
		return nil, fmt.Errorf("copy drift positive drift requires a positive budget")
	}
	remaining := input.SessionBudget
	legs := make([]legCanonical, 0, len(keys))
	prepared := decimal.Zero
	for _, key := range keys {
		current, target := currentMap[key], targetMap[key]
		delta := target.Sub(current)
		if delta.IsZero() || !remaining.IsPositive() {
			continue
		}
		requested := decimal.Min(delta.Abs(), remaining)
		var projected decimal.Decimal
		side := "buy"
		if delta.IsNegative() {
			side = "sell"
			projected = current.Sub(requested)
		} else {
			projected = current.Add(requested)
		}
		legs = append(legs, legCanonical{len(legs), key, side, money(current), money(target), money(delta.Abs()), money(requested), money(projected), money(target.Sub(projected).Abs())})
		prepared = prepared.Add(requested)
		remaining = remaining.Sub(requested)
	}
	residual := starting.Sub(prepared)
	canonical := runCanonical{SchemaV1, "prepared", sub.ID.String(), sub.OriginType, sub.OriginID.String(), input.SourceObservationID.String(), input.SessionKey, CalculationVersion, money(maximum), money(input.SessionBudget), money(starting), money(prepared), money(residual), residual.IsZero(), targets, currents, legs}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Run{canonical, encoded, digest, economicid.DeterministicUUID("copy-target-drift-session", SchemaV1+"@sha256:"+digest)}, nil
}

func FromCanonical(id uuid.UUID, digest string, raw []byte) (*Run, error) {
	var canonical runCanonical
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil || validateCanonical(canonical) != nil {
		return nil, fmt.Errorf("copy drift run envelope is invalid")
	}
	encoded, _ := json.Marshal(canonical)
	value := &Run{canonical, encoded, digest, economicid.DeterministicUUID("copy-target-drift-session", SchemaV1+"@sha256:"+digest)}
	if value.id != id || !bytes.Equal(encoded, raw) {
		return nil, fmt.Errorf("copy drift run identity does not reconstruct")
	}
	return value, nil
}

func validateCanonical(c runCanonical) error {
	subscriptionID, subErr := uuid.Parse(c.SubscriptionID)
	originID, originErr := uuid.Parse(c.OriginID)
	sourceID, sourceErr := uuid.Parse(c.SourceObservationID)
	if c.Schema != SchemaV1 || c.State != "prepared" || subErr != nil || originErr != nil || sourceErr != nil || subscriptionID == uuid.Nil || originID != subscriptionID || sourceID == uuid.Nil || c.OriginType != "copy_subscription" || !validSession(c.SessionKey) || c.CalculationVersion != CalculationVersion {
		return fmt.Errorf("attribution is invalid")
	}
	maximum, err := parseMoney(c.MaximumSessionTurnover)
	if err != nil || !maximum.IsPositive() {
		return fmt.Errorf("maximum turnover is invalid")
	}
	budget, err := parseMoney(c.SessionBudget)
	if err != nil || budget.IsNegative() || budget.GreaterThan(maximum) {
		return fmt.Errorf("budget is invalid")
	}
	targets, targetMap, err := normalizeCanonicalValues(c.Targets)
	if err != nil || !equalValueCanonical(targets, c.Targets) {
		return fmt.Errorf("targets are invalid")
	}
	currents, currentMap, err := normalizeCanonicalValues(c.Currents)
	if err != nil || !equalValueCanonical(currents, c.Currents) {
		return fmt.Errorf("currents are invalid")
	}
	keys := unionKeys(targetMap, currentMap)
	starting := decimal.Zero
	for _, key := range keys {
		starting = starting.Add(targetMap[key].Sub(currentMap[key]).Abs())
	}
	if starting.IsPositive() && !budget.IsPositive() {
		return fmt.Errorf("positive drift has no budget")
	}
	remaining, prepared := budget, decimal.Zero
	expected := make([]legCanonical, 0, len(keys))
	for _, key := range keys {
		current, target := currentMap[key], targetMap[key]
		delta := target.Sub(current)
		if delta.IsZero() || !remaining.IsPositive() {
			continue
		}
		requested := decimal.Min(delta.Abs(), remaining)
		projected, side := current.Add(requested), "buy"
		if delta.IsNegative() {
			projected, side = current.Sub(requested), "sell"
		}
		expected = append(expected, legCanonical{len(expected), key, side, money(current), money(target), money(delta.Abs()), money(requested), money(projected), money(target.Sub(projected).Abs())})
		prepared, remaining = prepared.Add(requested), remaining.Sub(requested)
	}
	residual := starting.Sub(prepared)
	if !equalLegs(expected, c.Legs) || c.StartingDrift != money(starting) || c.PreparedTurnover != money(prepared) || c.ResidualDrift != money(residual) || c.Converged != residual.IsZero() {
		return fmt.Errorf("arithmetic does not reconstruct")
	}
	return nil
}

func normalizeValues(values []Value) ([]valueCanonical, map[string]decimal.Decimal, error) {
	result := make([]valueCanonical, 0, len(values))
	seen := map[string]decimal.Decimal{}
	for _, value := range values {
		key := strings.ToUpper(strings.TrimSpace(value.InstrumentKey))
		if key == "" || value.Amount.IsNegative() || !validMoney(value.Amount) {
			return nil, nil, fmt.Errorf("value is invalid")
		}
		if _, ok := seen[key]; ok {
			return nil, nil, fmt.Errorf("instrument is duplicated")
		}
		seen[key] = value.Amount
		result = append(result, valueCanonical{key, money(value.Amount)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].InstrumentKey < result[j].InstrumentKey })
	return result, seen, nil
}

func normalizeCanonicalValues(values []valueCanonical) ([]valueCanonical, map[string]decimal.Decimal, error) {
	parsed := make([]Value, len(values))
	for i, value := range values {
		amount, err := parseMoney(value.Value)
		if err != nil || value.InstrumentKey != strings.ToUpper(strings.TrimSpace(value.InstrumentKey)) {
			return nil, nil, fmt.Errorf("canonical value is invalid")
		}
		parsed[i] = Value{value.InstrumentKey, amount}
	}
	return normalizeValues(parsed)
}

func unionKeys(left, right map[string]decimal.Decimal) []string {
	keys := make([]string, 0, len(left)+len(right))
	seen := map[string]bool{}
	for key := range left {
		seen[key] = true
		keys = append(keys, key)
	}
	for key := range right {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func validSession(value string) bool {
	match := sessionPattern.FindStringSubmatch(value)
	if len(match) != 3 {
		return false
	}
	_, err := time.Parse("2006-01-02", match[1])
	return err == nil
}

func validMoney(value decimal.Decimal) bool {
	return value.Equal(value.Round(2)) && len(value.String()) <= 128
}
func money(value decimal.Decimal) string { return value.Round(2).StringFixed(2) }
func parseMoney(value string) (decimal.Decimal, error) {
	parsed, err := decimal.NewFromString(value)
	if err != nil || !validMoney(parsed) || value != money(parsed) {
		return decimal.Zero, fmt.Errorf("invalid money")
	}
	return parsed, nil
}

func equalValueCanonical(left, right []valueCanonical) bool {
	return bytes.Equal(mustJSON(left), mustJSON(right))
}

func equalLegs(left, right []legCanonical) bool { return bytes.Equal(mustJSON(left), mustJSON(right)) }
func mustJSON(value any) []byte                 { raw, _ := json.Marshal(value); return raw }
func hash(value []byte) string                  { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }

func decodeExact(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("canonical JSON contains extra data")
	}
	return nil
}

func (r *Run) ID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return r.id
}

func (r *Run) Digest() string {
	if r == nil {
		return ""
	}
	return r.digest
}

func (r *Run) CanonicalBytes() json.RawMessage {
	if r == nil {
		return nil
	}
	return append(json.RawMessage(nil), r.bytes...)
}

func (r *Run) SessionKey() string {
	if r == nil {
		return ""
	}
	return r.canonical.SessionKey
}

func (r *Run) SourceObservationID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	value, _ := uuid.Parse(r.canonical.SourceObservationID)
	return value
}

func (r *Run) SubscriptionID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	value, _ := uuid.Parse(r.canonical.SubscriptionID)
	return value
}

func (r *Run) PreparedTurnover() string {
	if r == nil {
		return ""
	}
	return r.canonical.PreparedTurnover
}

func (r *Run) ResidualDrift() string {
	if r == nil {
		return ""
	}
	return r.canonical.ResidualDrift
}
func (r *Run) Converged() bool { return r != nil && r.canonical.Converged }
