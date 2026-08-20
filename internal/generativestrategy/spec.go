// Package generativestrategy owns constrained generated strategy specifications
// and deterministic compilation into immutable strategy-catalog versions. It
// cannot create experiments, deployments, intents, orders, or schedules.
package generativestrategy

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

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

const SpecSchemaV1 = "typed-generative-strategy-spec-v1"

var (
	tokenPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type InputField struct {
	Name             string
	Type             string
	DatasetKind      dataset.Kind
	Field            string
	FreshnessSeconds int64
	MissingPolicy    string
}

type Expr struct {
	Op    string
	Ref   string
	Value string
	Args  []Expr
}

type Universe struct {
	AssetClass  instrument.AssetClass
	Instruments []uuid.UUID
	Benchmark   uuid.UUID
}

type Sizing struct {
	Mode        string
	Value       string
	MaxPosition string
}

type Costs struct {
	SpreadBPS   string
	FeeBPS      string
	SlippageBPS string
}

type Capacity struct {
	MaximumDailyTurnover string
	MaximumParticipation string
}

type ExampleTest struct {
	Key           string
	Values        map[string]string
	ExpectedEntry bool
	ExpectedExit  bool
}

type Retirement struct {
	MaximumDrawdown     string
	MinimumSamples      int64
	MaximumFailedChecks int64
}

type Authoring struct {
	Provider     string
	Model        string
	PromptSHA256 string
	InputTokens  int64
	OutputTokens int64
	Currency     string
	Cost         string
}

type SpecInput struct {
	Family                *strategycatalog.Family
	SpecKey               string
	Inputs                []InputField
	Universe              Universe
	Entry                 Expr
	Exit                  Expr
	Sizing                Sizing
	MaximumHoldingSeconds int64
	Costs                 Costs
	Capacity              Capacity
	ProhibitedBehaviors   []string
	PropertyTests         []string
	ExampleTests          []ExampleTest
	Retirement            Retirement
	Authoring             Authoring
}

type inputCanonical struct {
	Name             string       `json:"name"`
	Type             string       `json:"type"`
	DatasetKind      dataset.Kind `json:"dataset_kind"`
	Field            string       `json:"field"`
	FreshnessSeconds int64        `json:"freshness_seconds"`
	MissingPolicy    string       `json:"missing_policy"`
}

type exprCanonical struct {
	Op    string          `json:"op"`
	Ref   string          `json:"ref"`
	Value string          `json:"value"`
	Args  []exprCanonical `json:"args"`
}

type universeCanonical struct {
	AssetClass  instrument.AssetClass `json:"asset_class"`
	Instruments []string              `json:"instruments"`
	Benchmark   string                `json:"benchmark"`
}

type (
	sizingCanonical struct {
		Mode        string `json:"mode"`
		Value       string `json:"value"`
		MaxPosition string `json:"max_position"`
	}
	costsCanonical struct {
		SpreadBPS   string `json:"spread_bps"`
		FeeBPS      string `json:"fee_bps"`
		SlippageBPS string `json:"slippage_bps"`
	}
	capacityCanonical struct {
		MaximumDailyTurnover string `json:"maximum_daily_turnover"`
		MaximumParticipation string `json:"maximum_participation"`
	}
	bindingCanonical struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	exampleCanonical struct {
		Key           string             `json:"key"`
		Values        []bindingCanonical `json:"values"`
		ExpectedEntry bool               `json:"expected_entry"`
		ExpectedExit  bool               `json:"expected_exit"`
	}
	retirementCanonical struct {
		MaximumDrawdown     string `json:"maximum_drawdown"`
		MinimumSamples      int64  `json:"minimum_samples"`
		MaximumFailedChecks int64  `json:"maximum_failed_checks"`
	}
	authoringCanonical struct {
		Provider     string `json:"provider"`
		Model        string `json:"model"`
		PromptSHA256 string `json:"prompt_sha256"`
		InputTokens  int64  `json:"input_tokens"`
		OutputTokens int64  `json:"output_tokens"`
		Currency     string `json:"currency"`
		Cost         string `json:"cost"`
	}
)

type specCanonical struct {
	Schema                string              `json:"schema"`
	FamilyID              string              `json:"family_id"`
	FamilySHA256          string              `json:"family_sha256"`
	SpecKey               string              `json:"spec_key"`
	Inputs                []inputCanonical    `json:"inputs"`
	Universe              universeCanonical   `json:"universe"`
	Entry                 exprCanonical       `json:"entry"`
	Exit                  exprCanonical       `json:"exit"`
	Sizing                sizingCanonical     `json:"sizing"`
	MaximumHoldingSeconds int64               `json:"maximum_holding_seconds"`
	Costs                 costsCanonical      `json:"costs"`
	Capacity              capacityCanonical   `json:"capacity"`
	ProhibitedBehaviors   []string            `json:"prohibited_behaviors"`
	PropertyTests         []string            `json:"property_tests"`
	ExampleTests          []exampleCanonical  `json:"example_tests"`
	Retirement            retirementCanonical `json:"retirement"`
	Authoring             authoringCanonical  `json:"authoring"`
}

type Spec struct {
	canonical specCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

var (
	requiredProhibitions = []string{"evidence_mutation", "live_order_submission", "lookahead", "network_access", "promotion", "risk_limit_mutation", "secret_access"}
	requiredProperties   = []string{"cost_hurdle_required", "missing_input_abstains", "no_lookahead", "size_bounded", "stale_input_abstains"}
)

func NewSpec(input SpecInput) (*Spec, error) {
	if input.Family == nil || !tokenPattern.MatchString(input.SpecKey) || input.MaximumHoldingSeconds <= 0 || input.MaximumHoldingSeconds > 10*365*24*3600 {
		return nil, fmt.Errorf("generated strategy spec identity is invalid")
	}
	inputs, types, err := normalizeInputs(input.Inputs)
	if err != nil {
		return nil, err
	}
	universe, err := normalizeUniverse(input.Universe, input.Family)
	if err != nil {
		return nil, err
	}
	nodes := 0
	entry, kind, err := normalizeExpr(input.Entry, types, 0, &nodes)
	if err != nil || kind != "boolean" {
		return nil, fmt.Errorf("generated strategy entry is invalid")
	}
	nodes = 0
	exit, kind, err := normalizeExpr(input.Exit, types, 0, &nodes)
	if err != nil || kind != "boolean" {
		return nil, fmt.Errorf("generated strategy exit is invalid")
	}
	sizing, err := normalizeSizing(input.Sizing)
	if err != nil {
		return nil, err
	}
	costs, capacity, err := normalizeEconomics(input.Costs, input.Capacity)
	if err != nil {
		return nil, err
	}
	prohibitions, err := normalizeRequiredSet(input.ProhibitedBehaviors, requiredProhibitions, "prohibited behavior")
	if err != nil {
		return nil, err
	}
	properties, err := normalizeRequiredSet(input.PropertyTests, requiredProperties, "property test")
	if err != nil {
		return nil, err
	}
	examples, err := normalizeExamples(input.ExampleTests, types)
	if err != nil {
		return nil, err
	}
	retirement, err := normalizeRetirement(input.Retirement)
	if err != nil {
		return nil, err
	}
	authoring, err := normalizeAuthoring(input.Authoring)
	if err != nil {
		return nil, err
	}
	canonical := specCanonical{SpecSchemaV1, input.Family.ID().String(), input.Family.Digest(), input.SpecKey, inputs, universe, entry, exit, sizing, input.MaximumHoldingSeconds, costs, capacity, prohibitions, properties, examples, retirement, authoring}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal generated strategy spec: %w", err)
	}
	digest := hash(encoded)
	return &Spec{canonical, encoded, digest, economicid.DeterministicUUID("typed-generative-strategy-spec", SpecSchemaV1+"@sha256:"+digest)}, nil
}

func normalizeInputs(values []InputField) ([]inputCanonical, map[string]string, error) {
	if len(values) == 0 || len(values) > 64 {
		return nil, nil, fmt.Errorf("generated strategy inputs are invalid")
	}
	result := make([]inputCanonical, 0, len(values))
	for _, value := range values {
		if !tokenPattern.MatchString(value.Name) || value.Type != "decimal" && value.Type != "boolean" || !validDatasetKind(value.DatasetKind) || !tokenPattern.MatchString(value.Field) || value.FreshnessSeconds <= 0 || value.FreshnessSeconds > 365*24*3600 || value.MissingPolicy != "abstain" {
			return nil, nil, fmt.Errorf("generated strategy input is invalid")
		}
		result = append(result, inputCanonical(value))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	types := map[string]string{}
	for _, value := range result {
		if _, exists := types[value.Name]; exists {
			return nil, nil, fmt.Errorf("generated strategy input is duplicated")
		}
		types[value.Name] = value.Type
	}
	return result, types, nil
}

func normalizeUniverse(value Universe, family *strategycatalog.Family) (universeCanonical, error) {
	validClass := false
	for _, class := range family.AssetClasses() {
		if class == value.AssetClass {
			validClass = true
		}
	}
	if !validClass || value.Benchmark == uuid.Nil || len(value.Instruments) == 0 || len(value.Instruments) > 1024 {
		return universeCanonical{}, fmt.Errorf("generated strategy universe is invalid")
	}
	ids := append([]uuid.UUID(nil), value.Instruments...)
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	result := universeCanonical{AssetClass: value.AssetClass, Benchmark: value.Benchmark.String()}
	for i, id := range ids {
		if id == uuid.Nil || i > 0 && ids[i-1] == id {
			return universeCanonical{}, fmt.Errorf("generated strategy universe is invalid")
		}
		result.Instruments = append(result.Instruments, id.String())
	}
	return result, nil
}

func normalizeExpr(value Expr, types map[string]string, depth int, nodes *int) (exprCanonical, string, error) {
	*nodes++
	if depth > 16 || *nodes > 128 {
		return exprCanonical{}, "", fmt.Errorf("generated strategy expression is too large")
	}
	row := exprCanonical{Op: value.Op, Ref: value.Ref, Value: value.Value, Args: []exprCanonical{}}
	switch value.Op {
	case "ref":
		kind, ok := types[value.Ref]
		if !ok || value.Value != "" || len(value.Args) != 0 {
			return row, "", fmt.Errorf("generated strategy reference is invalid")
		}
		return row, kind, nil
	case "decimal":
		if value.Ref != "" || len(value.Args) != 0 {
			return row, "", fmt.Errorf("generated strategy literal is invalid")
		}
		parsed, err := exactDecimal(value.Value)
		if err != nil {
			return row, "", err
		}
		row.Value = parsed.String()
		return row, "decimal", nil
	case "boolean":
		if value.Ref != "" || len(value.Args) != 0 || value.Value != "true" && value.Value != "false" {
			return row, "", fmt.Errorf("generated strategy literal is invalid")
		}
		return row, "boolean", nil
	}
	if value.Ref != "" || value.Value != "" {
		return row, "", fmt.Errorf("generated strategy operator payload is invalid")
	}
	expected, output, arity := "decimal", "decimal", 2
	switch value.Op {
	case "add", "sub", "mul", "div":
	case "lt", "lte", "gt", "gte", "eq":
		output = "boolean"
	case "and", "or":
		expected, output = "boolean", "boolean"
	case "not":
		expected, output, arity = "boolean", "boolean", 1
	default:
		return row, "", fmt.Errorf("generated strategy operator is invalid")
	}
	if len(value.Args) != arity {
		return row, "", fmt.Errorf("generated strategy operator arity is invalid")
	}
	for _, child := range value.Args {
		normalized, kind, err := normalizeExpr(child, types, depth+1, nodes)
		if err != nil || kind != expected {
			return row, "", fmt.Errorf("generated strategy expression type is invalid")
		}
		row.Args = append(row.Args, normalized)
	}
	if value.Op == "div" && row.Args[1].Op == "decimal" && row.Args[1].Value == "0" {
		return row, "", fmt.Errorf("generated strategy literal division by zero")
	}
	return row, output, nil
}

func normalizeSizing(value Sizing) (sizingCanonical, error) {
	amount, aerr := exactDecimal(value.Value)
	maximum, merr := exactDecimal(value.MaxPosition)
	if value.Mode != "fixed_fraction" && value.Mode != "fixed_notional" || aerr != nil || merr != nil || !amount.GreaterThan(decimal.Zero) || !maximum.GreaterThan(decimal.Zero) || amount.GreaterThan(maximum) {
		return sizingCanonical{}, fmt.Errorf("generated strategy sizing is invalid")
	}
	return sizingCanonical{value.Mode, amount.String(), maximum.String()}, nil
}

func normalizeEconomics(cost Costs, capacity Capacity) (costsCanonical, capacityCanonical, error) {
	spread, e1 := exactDecimal(cost.SpreadBPS)
	fee, e2 := exactDecimal(cost.FeeBPS)
	slippage, e3 := exactDecimal(cost.SlippageBPS)
	turnover, e4 := exactDecimal(capacity.MaximumDailyTurnover)
	participation, e5 := exactDecimal(capacity.MaximumParticipation)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || spread.IsNegative() || fee.IsNegative() || slippage.IsNegative() || !turnover.GreaterThan(decimal.Zero) || !participation.GreaterThan(decimal.Zero) || participation.GreaterThan(decimal.NewFromInt(1)) {
		return costsCanonical{}, capacityCanonical{}, fmt.Errorf("generated strategy economics are invalid")
	}
	return costsCanonical{spread.String(), fee.String(), slippage.String()}, capacityCanonical{turnover.String(), participation.String()}, nil
}

func normalizeRequiredSet(values, required []string, label string) ([]string, error) {
	result := append([]string(nil), values...)
	sort.Strings(result)
	seen := map[string]bool{}
	for _, value := range result {
		if !tokenPattern.MatchString(value) || seen[value] {
			return nil, fmt.Errorf("generated strategy %s is invalid", label)
		}
		seen[value] = true
	}
	for _, value := range required {
		if !seen[value] {
			return nil, fmt.Errorf("generated strategy required %s is missing", label)
		}
	}
	return result, nil
}

func normalizeExamples(values []ExampleTest, types map[string]string) ([]exampleCanonical, error) {
	if len(values) == 0 || len(values) > 128 {
		return nil, fmt.Errorf("generated strategy examples are invalid")
	}
	result := make([]exampleCanonical, 0, len(values))
	for _, value := range values {
		if !tokenPattern.MatchString(value.Key) || len(value.Values) != len(types) {
			return nil, fmt.Errorf("generated strategy example is invalid")
		}
		row := exampleCanonical{Key: value.Key, ExpectedEntry: value.ExpectedEntry, ExpectedExit: value.ExpectedExit}
		for name, kind := range types {
			raw, ok := value.Values[name]
			if !ok {
				return nil, fmt.Errorf("generated strategy example binding is missing")
			}
			if kind == "decimal" {
				parsed, err := exactDecimal(raw)
				if err != nil {
					return nil, err
				}
				raw = parsed.String()
			} else if raw != "true" && raw != "false" {
				return nil, fmt.Errorf("generated strategy boolean example is invalid")
			}
			row.Values = append(row.Values, bindingCanonical{name, raw})
		}
		sort.Slice(row.Values, func(i, j int) bool { return row.Values[i].Name < row.Values[j].Name })
		result = append(result, row)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	for i := 1; i < len(result); i++ {
		if result[i-1].Key == result[i].Key {
			return nil, fmt.Errorf("generated strategy example is duplicated")
		}
	}
	return result, nil
}

func normalizeRetirement(value Retirement) (retirementCanonical, error) {
	drawdown, err := exactDecimal(value.MaximumDrawdown)
	if err != nil || !drawdown.GreaterThan(decimal.Zero) || drawdown.GreaterThan(decimal.NewFromInt(1)) || value.MinimumSamples <= 0 || value.MaximumFailedChecks <= 0 {
		return retirementCanonical{}, fmt.Errorf("generated strategy retirement is invalid")
	}
	return retirementCanonical{drawdown.String(), value.MinimumSamples, value.MaximumFailedChecks}, nil
}

func normalizeAuthoring(value Authoring) (authoringCanonical, error) {
	cost, err := exactDecimal(value.Cost)
	if !tokenPattern.MatchString(value.Provider) || value.Model == "" || value.Model != strings.TrimSpace(value.Model) || len(value.Model) > 128 || !digestPattern.MatchString(value.PromptSHA256) || value.InputTokens < 0 || value.OutputTokens < 0 || !regexp.MustCompile(`^[A-Z]{3}$`).MatchString(value.Currency) || err != nil || cost.IsNegative() {
		return authoringCanonical{}, fmt.Errorf("generated strategy authoring provenance is invalid")
	}
	return authoringCanonical{value.Provider, value.Model, value.PromptSHA256, value.InputTokens, value.OutputTokens, value.Currency, cost.String()}, nil
}

func validDatasetKind(value dataset.Kind) bool {
	switch value {
	case dataset.KindBars, dataset.KindBenchmarkMembership, dataset.KindCorporateActions,
		dataset.KindDepth, dataset.KindExternalObject, dataset.KindFilings,
		dataset.KindFundamentals, dataset.KindOptionChains, dataset.KindOptionContracts,
		dataset.KindPredictionBooks, dataset.KindPredictionFees, dataset.KindPredictionRules,
		dataset.KindPredictionTrades, dataset.KindQuotes, dataset.KindResolutions:
		return true
	default:
		return false
	}
}

func exactDecimal(value string) (decimal.Decimal, error) {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "eE+") {
		return decimal.Zero, fmt.Errorf("generated strategy decimal is not canonical")
	}
	parsed, err := decimal.NewFromString(value)
	if err != nil || parsed.String() != value || parsed.Exponent() < -12 {
		return decimal.Zero, fmt.Errorf("generated strategy decimal is not canonical")
	}
	return parsed, nil
}
func hash(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }

func (s *Spec) ID() uuid.UUID {
	if s == nil {
		return uuid.Nil
	}
	return s.id
}

func (s *Spec) Digest() string {
	if s == nil {
		return ""
	}
	return s.digest
}

func (s *Spec) CanonicalBytes() json.RawMessage {
	if s == nil {
		return nil
	}
	return append(json.RawMessage(nil), s.bytes...)
}

func (s *Spec) FamilyID() uuid.UUID {
	if s == nil {
		return uuid.Nil
	}
	return uuid.MustParse(s.canonical.FamilyID)
}

func (s *Spec) FamilyDigest() string {
	if s == nil {
		return ""
	}
	return s.canonical.FamilySHA256
}

func (s *Spec) RequiredDatasetKinds() []dataset.Kind {
	if s == nil {
		return nil
	}
	values := make([]dataset.Kind, 0, len(s.canonical.Inputs))
	seen := map[dataset.Kind]bool{}
	for _, input := range s.canonical.Inputs {
		if !seen[input.DatasetKind] {
			seen[input.DatasetKind] = true
			values = append(values, input.DatasetKind)
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values
}

func SpecFromCanonical(id uuid.UUID, digest string, raw []byte, family *strategycatalog.Family) (*Spec, error) {
	var canonical specCanonical
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if id == uuid.Nil || family == nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decoder.Decode(&canonical) != nil {
		return nil, fmt.Errorf("generated strategy spec envelope is invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("generated strategy spec has extra JSON")
	}
	input, err := canonical.toInput(family)
	if err != nil {
		return nil, err
	}
	rebuilt, err := NewSpec(input)
	if err != nil || canonical.Schema != SpecSchemaV1 || rebuilt.ID() != id || rebuilt.Digest() != digest || !bytes.Equal(rebuilt.bytes, raw) {
		return nil, fmt.Errorf("generated strategy spec does not reconstruct")
	}
	return rebuilt, nil
}

func (value specCanonical) toInput(family *strategycatalog.Family) (SpecInput, error) {
	if value.FamilyID != family.ID().String() || value.FamilySHA256 != family.Digest() {
		return SpecInput{}, fmt.Errorf("generated strategy family binding is invalid")
	}
	result := SpecInput{Family: family, SpecKey: value.SpecKey, MaximumHoldingSeconds: value.MaximumHoldingSeconds, Sizing: Sizing{value.Sizing.Mode, value.Sizing.Value, value.Sizing.MaxPosition}, Costs: Costs{value.Costs.SpreadBPS, value.Costs.FeeBPS, value.Costs.SlippageBPS}, Capacity: Capacity{value.Capacity.MaximumDailyTurnover, value.Capacity.MaximumParticipation}, ProhibitedBehaviors: append([]string(nil), value.ProhibitedBehaviors...), PropertyTests: append([]string(nil), value.PropertyTests...), Retirement: Retirement{value.Retirement.MaximumDrawdown, value.Retirement.MinimumSamples, value.Retirement.MaximumFailedChecks}, Authoring: Authoring{value.Authoring.Provider, value.Authoring.Model, value.Authoring.PromptSHA256, value.Authoring.InputTokens, value.Authoring.OutputTokens, value.Authoring.Currency, value.Authoring.Cost}}
	for _, row := range value.Inputs {
		result.Inputs = append(result.Inputs, InputField(row))
	}
	benchmark, err := uuid.Parse(value.Universe.Benchmark)
	if err != nil {
		return SpecInput{}, fmt.Errorf("generated strategy benchmark identity is invalid")
	}
	result.Universe.AssetClass, result.Universe.Benchmark = value.Universe.AssetClass, benchmark
	for _, raw := range value.Universe.Instruments {
		id, parseErr := uuid.Parse(raw)
		if parseErr != nil {
			return SpecInput{}, parseErr
		}
		result.Universe.Instruments = append(result.Universe.Instruments, id)
	}
	result.Entry, result.Exit = exprInput(value.Entry), exprInput(value.Exit)
	for _, row := range value.ExampleTests {
		example := ExampleTest{Key: row.Key, Values: map[string]string{}, ExpectedEntry: row.ExpectedEntry, ExpectedExit: row.ExpectedExit}
		for _, binding := range row.Values {
			example.Values[binding.Name] = binding.Value
		}
		result.ExampleTests = append(result.ExampleTests, example)
	}
	return result, nil
}

func exprInput(value exprCanonical) Expr {
	result := Expr{Op: value.Op, Ref: value.Ref, Value: value.Value}
	for _, child := range value.Args {
		result.Args = append(result.Args, exprInput(child))
	}
	return result
}
