package simulation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
)

func TestPolicyVersionIsCanonicalAndContentAddressed(t *testing.T) {
	input := validPolicyInput()
	policy, err := NewPolicy(input)
	if err != nil {
		t.Fatal(err)
	}

	if policy.Schema() != PolicySchemaV1 {
		t.Fatalf("Schema() = %q, want %q", policy.Schema(), PolicySchemaV1)
	}
	if !strings.HasPrefix(policy.Version(), PolicySchemaV1+"@sha256:") || len(policy.Digest()) != 64 {
		t.Fatalf("policy version/digest = %q/%q", policy.Version(), policy.Digest())
	}
	if want := economicid.DeterministicUUID("simulation-policy-artifact", policy.Version()); policy.ArtifactID() != want {
		t.Fatalf("ArtifactID() = %s, want %s", policy.ArtifactID(), want)
	}

	var decoded struct {
		Schema string `json:"schema"`
		Assets []any  `json:"assets"`
	}
	if err := json.Unmarshal(policy.CanonicalBytes(), &decoded); err != nil {
		t.Fatalf("canonical bytes are not JSON: %v", err)
	}
	if decoded.Schema != PolicySchemaV1 || len(decoded.Assets) != len(input.Assets) {
		t.Fatalf("canonical policy = %#v", decoded)
	}

	bytes := policy.CanonicalBytes()
	bytes[0] = '['
	if string(bytes) == string(policy.CanonicalBytes()) {
		t.Fatal("CanonicalBytes() exposed mutable policy storage")
	}
}

func TestPolicyArtifactIDUsesFullVersion(t *testing.T) {
	first, err := NewPolicy(validPolicyInput())
	if err != nil {
		t.Fatal(err)
	}
	changedInput := validPolicyInput()
	changedInput.Assets[0].Fees.NotionalBPS = decimal.RequireFromString("2.5")
	second, err := NewPolicy(changedInput)
	if err != nil {
		t.Fatal(err)
	}
	if first.Version() == second.Version() || first.ArtifactID() == second.ArtifactID() {
		t.Fatalf("materially different policies shared identity: %q/%s", first.Version(), first.ArtifactID())
	}
}

func TestPolicyVersionIgnoresInputOrderingButNotEconomics(t *testing.T) {
	firstInput := validPolicyInput()
	first, err := NewPolicy(firstInput)
	if err != nil {
		t.Fatal(err)
	}

	secondInput := validPolicyInput()
	secondInput.Assets[0], secondInput.Assets[1] = secondInput.Assets[1], secondInput.Assets[0]
	for index := range secondInput.Assets {
		reverseOrderTypes(secondInput.Assets[index].OrderTypes)
		reverseTimeInForce(secondInput.Assets[index].TimeInForce)
		reverseStrings(secondInput.Assets[index].QuoteRequirements.AllowedMarketStatuses)
		secondInput.Assets[index].QuoteRequirements.AllowedMarketStatuses = append(
			secondInput.Assets[index].QuoteRequirements.AllowedMarketStatuses,
			secondInput.Assets[index].QuoteRequirements.AllowedMarketStatuses[0],
		)
		reverseStrings(secondInput.Assets[index].QuoteRequirements.AllowedSessionStatuses)
		reverseSessions(secondInput.Assets[index].Calendar.Sessions)
	}
	second, err := NewPolicy(secondInput)
	if err != nil {
		t.Fatal(err)
	}
	if first.Version() != second.Version() || string(first.CanonicalBytes()) != string(second.CanonicalBytes()) {
		t.Fatalf("ordering changed canonical identity:\n%s\n%s", first.CanonicalBytes(), second.CanonicalBytes())
	}

	changedInput := validPolicyInput()
	changedInput.Assets[0].FixedLatency += time.Microsecond
	changed, err := NewPolicy(changedInput)
	if err != nil {
		t.Fatal(err)
	}
	if first.Version() == changed.Version() {
		t.Fatal("economic latency change did not change policy version")
	}
}

func TestPolicyRejectsDuplicateOrUnsupportedAssetCapabilities(t *testing.T) {
	tests := map[string]func(*PolicyInput){
		"duplicate asset": func(input *PolicyInput) {
			input.Assets = append(input.Assets, input.Assets[0])
		},
		"unsupported future": func(input *PolicyInput) {
			input.Assets[0].AssetClass = instrument.AssetClassFuture
		},
		"unknown asset": func(input *PolicyInput) {
			input.Assets[0].AssetClass = instrument.AssetClassUnknown
		},
		"duplicate order type": func(input *PolicyInput) {
			input.Assets[0].OrderTypes = append(input.Assets[0].OrderTypes, lifecycle.OrderMarket)
		},
		"unsupported stop": func(input *PolicyInput) {
			input.Assets[0].OrderTypes = append(input.Assets[0].OrderTypes, lifecycle.OrderStop)
		},
		"duplicate time in force": func(input *PolicyInput) {
			input.Assets[0].TimeInForce = append(input.Assets[0].TimeInForce, lifecycle.TimeInForceGTC)
		},
		"unsupported gtd": func(input *PolicyInput) {
			input.Assets[0].TimeInForce = append(input.Assets[0].TimeInForce, lifecycle.TimeInForceGTD)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input := validPolicyInput()
			mutate(&input)
			if _, err := NewPolicy(input); err == nil {
				t.Fatal("NewPolicy() unexpectedly succeeded")
			}
		})
	}
}

func TestPolicyValidatesExplicitSessionsHolidaysAndHalfDays(t *testing.T) {
	input := validPolicyInput()
	policy, err := NewPolicy(input)
	if err != nil {
		t.Fatal(err)
	}

	normalRoute := policyTestTime().Add(90 * time.Minute)
	normal, err := policy.RouteSession(instrument.AssetClassEquity, normalRoute)
	if err != nil {
		t.Fatal(err)
	}
	if normal.Label != "regular-2026-08-17" || !normal.CloseAt.Equal(policyTestTime().Add(6*time.Hour)) {
		t.Fatalf("normal session = %#v", normal)
	}
	halfDayRoute := policyTestTime().Add(24*time.Hour + 90*time.Minute)
	halfDay, err := policy.RouteSession(instrument.AssetClassEquity, halfDayRoute)
	if err != nil {
		t.Fatal(err)
	}
	if halfDay.Label != "half-day-2026-08-18" || !halfDay.CloseAt.Equal(policyTestTime().Add(27*time.Hour+30*time.Minute)) {
		t.Fatalf("half-day session = %#v", halfDay)
	}
	if _, err := policy.RouteSession(instrument.AssetClassEquity, policyTestTime().Add(49*time.Hour)); err == nil {
		t.Fatal("holiday route unexpectedly resolved a session")
	}

	overlap := validPolicyInput()
	overlap.Assets[0].Calendar.Sessions[1].OpenAt = overlap.Assets[0].Calendar.Sessions[0].CloseAt.Add(-time.Minute)
	if _, err := NewPolicy(overlap); err == nil {
		t.Fatal("overlapping sessions unexpectedly succeeded")
	}

	localTime := validPolicyInput()
	location := time.FixedZone("not-utc", -5*60*60)
	localTime.Assets[0].Calendar.Sessions[0].OpenAt = localTime.Assets[0].Calendar.Sessions[0].OpenAt.In(location)
	if _, err := NewPolicy(localTime); err == nil {
		t.Fatal("non-UTC session unexpectedly succeeded")
	}

	nonMicrosecond := validPolicyInput()
	nonMicrosecond.Assets[0].Calendar.Sessions[0].CloseAt = nonMicrosecond.Assets[0].Calendar.Sessions[0].CloseAt.Add(time.Nanosecond)
	if _, err := NewPolicy(nonMicrosecond); err == nil {
		t.Fatal("non-microsecond session unexpectedly succeeded")
	}
}

func TestContinuous24x7PolicyRejectsDAY(t *testing.T) {
	input := validPolicyInput()
	crypto := input.Assets[1]
	crypto.TimeInForce = append(crypto.TimeInForce, lifecycle.TimeInForceDay)
	input.Assets[1] = crypto
	if _, err := NewPolicy(input); err == nil {
		t.Fatal("continuous 24/7 policy with DAY unexpectedly succeeded")
	}

	input = validPolicyInput()
	input.Assets[1].Calendar.Sessions = []SessionWindow{{
		Label: "forbidden", OpenAt: policyTestTime(), CloseAt: policyTestTime().Add(time.Hour),
	}}
	if _, err := NewPolicy(input); err == nil {
		t.Fatal("continuous 24/7 policy with explicit sessions unexpectedly succeeded")
	}
}

func TestPolicyRequiresExplicitQuoteDepthStatusAndAgeRules(t *testing.T) {
	tests := map[string]func(*marketdata.QuoteRequirements){
		"source":         func(value *marketdata.QuoteRequirements) { value.RequireSource = false },
		"contract":       func(value *marketdata.QuoteRequirements) { value.RequireVenueContract = false },
		"bid":            func(value *marketdata.QuoteRequirements) { value.RequireBid = false },
		"ask":            func(value *marketdata.QuoteRequirements) { value.RequireAsk = false },
		"bid depth":      func(value *marketdata.QuoteRequirements) { value.RequireBidDepth = false },
		"ask depth":      func(value *marketdata.QuoteRequirements) { value.RequireAskDepth = false },
		"market status":  func(value *marketdata.QuoteRequirements) { value.RequireMarketStatus = false },
		"session status": func(value *marketdata.QuoteRequirements) { value.RequireSessionStatus = false },
		"market allow":   func(value *marketdata.QuoteRequirements) { value.AllowedMarketStatuses = nil },
		"session allow":  func(value *marketdata.QuoteRequirements) { value.AllowedSessionStatuses = nil },
		"positive age":   func(value *marketdata.QuoteRequirements) { value.MaxAge = 0 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input := validPolicyInput()
			mutate(&input.Assets[0].QuoteRequirements)
			if _, err := NewPolicy(input); err == nil {
				t.Fatal("NewPolicy() unexpectedly succeeded")
			}
		})
	}
}

func TestPolicyRejectsImplicitOrInexactFeeAndParticipationValues(t *testing.T) {
	tests := map[string]func(*AssetPolicy){
		"zero participation": func(value *AssetPolicy) { value.MaxDepthParticipation = decimal.Zero },
		"participation above one": func(value *AssetPolicy) {
			value.MaxDepthParticipation = decimal.RequireFromString("1.000000000001")
		},
		"inexact participation": func(value *AssetPolicy) {
			value.MaxDepthParticipation = decimal.RequireFromString("0.1234567890123")
		},
		"negative latency": func(value *AssetPolicy) { value.FixedLatency = -time.Microsecond },
		"negative fee":     func(value *AssetPolicy) { value.Fees.PerOrder = decimal.NewFromInt(-1) },
		"inexact fee": func(value *AssetPolicy) {
			value.Fees.PerUnit = decimal.RequireFromString("0.0000000000001")
		},
		"invalid fee scale": func(value *AssetPolicy) { value.Fees.Scale = 13 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input := validPolicyInput()
			mutate(&input.Assets[0])
			if _, err := NewPolicy(input); err == nil {
				t.Fatal("NewPolicy() unexpectedly succeeded")
			}
		})
	}
}

func TestPolicyComputesExactFirstAndLaterFillFees(t *testing.T) {
	policy, err := NewPolicy(validPolicyInput())
	if err != nil {
		t.Fatal(err)
	}

	firstFee, err := policy.FillFee(
		instrument.AssetClassEquity,
		decimal.NewFromInt(3),
		decimal.RequireFromString("10.25"),
		decimal.NewFromInt(1),
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if firstFee == nil || !firstFee.Equal(decimal.RequireFromString("1.2862")) {
		t.Fatalf("first fee = %v, want 1.2862", firstFee)
	}

	laterFee, err := policy.FillFee(
		instrument.AssetClassEquity,
		decimal.NewFromInt(3),
		decimal.RequireFromString("10.25"),
		decimal.NewFromInt(1),
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if laterFee == nil || !laterFee.Equal(decimal.RequireFromString("0.0362")) {
		t.Fatalf("later fee = %v, want 0.0362", laterFee)
	}

	zeroInput := validPolicyInput()
	zeroInput.Assets[0].Fees = FeePolicy{Scale: 4}
	zeroPolicy, err := NewPolicy(zeroInput)
	if err != nil {
		t.Fatal(err)
	}
	zeroFee, err := zeroPolicy.FillFee(
		instrument.AssetClassEquity,
		decimal.NewFromInt(1),
		decimal.NewFromInt(1),
		decimal.NewFromInt(1),
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if zeroFee != nil {
		t.Fatalf("zero fee = %s, want nil", zeroFee)
	}

	if _, err := policy.FillFee(instrument.AssetClassEquity, decimal.Zero, decimal.NewFromInt(1), decimal.NewFromInt(1), true); err == nil {
		t.Fatal("zero fill quantity unexpectedly accepted")
	}
}

func TestPolicyRoundTripsFromCanonicalArtifactBytes(t *testing.T) {
	policy, err := NewPolicy(validPolicyInput())
	if err != nil {
		t.Fatal(err)
	}
	createdAt := policyTestTime().Add(-time.Hour)
	artifact, err := policy.NewArtifact(createdAt)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.ID == uuid.Nil || artifact.ID != policy.ArtifactID() || artifact.Version != policy.Version() ||
		artifact.SHA256 != policy.Digest() || !artifact.CreatedAt.Equal(createdAt) {
		t.Fatalf("artifact = %#v", artifact)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatal(err)
	}

	restored, err := PolicyFromArtifact(*artifact)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Version() != policy.Version() || string(restored.CanonicalBytes()) != string(policy.CanonicalBytes()) {
		t.Fatalf("restored policy = %q/%s", restored.Version(), restored.CanonicalBytes())
	}

	changed := *artifact
	changed.CanonicalBytes = append(json.RawMessage(nil), artifact.CanonicalBytes...)
	changed.CanonicalBytes = append(changed.CanonicalBytes, ' ')
	if _, err := PolicyFromArtifact(changed); err == nil {
		t.Fatal("noncanonical changed artifact bytes unexpectedly restored")
	}
}

func validPolicyInput() PolicyInput {
	base := policyTestTime()
	quoteRequirements := marketdata.QuoteRequirements{
		RequireSource:          true,
		RequireVenueContract:   true,
		RequireBid:             true,
		RequireAsk:             true,
		RequireBidDepth:        true,
		RequireAskDepth:        true,
		RequireMarketStatus:    true,
		RequireSessionStatus:   true,
		AllowedMarketStatuses:  []string{"open", "continuous"},
		AllowedSessionStatuses: []string{"regular", "extended"},
		MaxAge:                 2 * time.Second,
	}
	return PolicyInput{
		Schema: PolicySchemaV1,
		Assets: []AssetPolicy{
			{
				AssetClass: instrument.AssetClassEquity,
				OrderTypes: []lifecycle.OrderType{lifecycle.OrderMarket, lifecycle.OrderLimit},
				TimeInForce: []lifecycle.TimeInForce{
					lifecycle.TimeInForceGTC,
					lifecycle.TimeInForceDay,
					lifecycle.TimeInForceIOC,
					lifecycle.TimeInForceFOK,
				},
				QuoteRequirements:     quoteRequirements,
				MaxDepthParticipation: decimal.RequireFromString("0.25"),
				FixedLatency:          40 * time.Millisecond,
				Calendar: CalendarPolicy{
					Kind: CalendarExplicitSessions,
					Sessions: []SessionWindow{
						{Label: "regular-2026-08-17", OpenAt: base.Add(time.Hour), CloseAt: base.Add(6 * time.Hour)},
						{Label: "half-day-2026-08-18", OpenAt: base.Add(25 * time.Hour), CloseAt: base.Add(27*time.Hour + 30*time.Minute)},
					},
				},
				Fees: FeePolicy{
					PerOrder:    decimal.RequireFromString("1.25"),
					PerUnit:     decimal.RequireFromString("0.01"),
					NotionalBPS: decimal.RequireFromString("2"),
					Scale:       4,
				},
			},
			{
				AssetClass: instrument.AssetClassCryptoSpot,
				OrderTypes: []lifecycle.OrderType{lifecycle.OrderMarket, lifecycle.OrderLimit},
				TimeInForce: []lifecycle.TimeInForce{
					lifecycle.TimeInForceGTC,
					lifecycle.TimeInForceIOC,
					lifecycle.TimeInForceFOK,
				},
				QuoteRequirements:     quoteRequirements,
				MaxDepthParticipation: decimal.RequireFromString("0.10"),
				FixedLatency:          20 * time.Millisecond,
				Calendar:              CalendarPolicy{Kind: CalendarContinuous24x7},
				Fees: FeePolicy{
					NotionalBPS: decimal.RequireFromString("10"),
					Scale:       8,
				},
			},
		},
	}
}

func policyTestTime() time.Time {
	return time.Date(2026, 8, 17, 12, 0, 0, 123456000, time.UTC)
}

func reverseOrderTypes(values []lifecycle.OrderType) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func reverseTimeInForce(values []lifecycle.TimeInForce) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func reverseStrings(values []string) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func reverseSessions(values []SessionWindow) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
