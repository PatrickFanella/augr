package capital

import (
	"bytes"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
)

func TestAssessCapitalProfilesAtExactBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		profile    domain.MarginProfile
		multiplier decimal.Decimal
		direction  ExposureDirection
		notional   decimal.Decimal
		want       Decision
		wantReason ReasonCode
		wantMargin decimal.Decimal
		wantBP     decimal.Decimal
	}{
		{
			name: "cash long admitted", profile: domain.MarginProfileCash, multiplier: decimal.NewFromInt(1),
			direction: ExposureIncreaseLong, notional: decimal.NewFromInt(80_000),
			want: DecisionAdmitted, wantReason: ReasonAdmitted, wantMargin: decimal.NewFromInt(80_000), wantBP: decimal.NewFromInt(100_000),
		},
		{
			name: "cash settled cash rejected", profile: domain.MarginProfileCash, multiplier: decimal.NewFromInt(1),
			direction: ExposureIncreaseLong, notional: decimal.NewFromInt(120_000),
			want: DecisionRejected, wantReason: ReasonInsufficientSettledCash, wantMargin: decimal.NewFromInt(120_000), wantBP: decimal.NewFromInt(100_000),
		},
		{
			name: "cash short rejected", profile: domain.MarginProfileCash, multiplier: decimal.NewFromInt(1),
			direction: ExposureIncreaseShort, notional: decimal.NewFromInt(1),
			want: DecisionRejected, wantReason: ReasonShortNotSupported, wantMargin: decimal.Zero, wantBP: decimal.Zero,
		},
		{
			name: "reg t long boundary", profile: domain.MarginProfileRegT, multiplier: decimal.NewFromInt(2),
			direction: ExposureIncreaseLong, notional: decimal.NewFromInt(200_000),
			want: DecisionAdmitted, wantReason: ReasonAdmitted, wantMargin: decimal.NewFromInt(100_000), wantBP: decimal.NewFromInt(200_000),
		},
		{
			name: "reg t long over boundary", profile: domain.MarginProfileRegT, multiplier: decimal.NewFromInt(2),
			direction: ExposureIncreaseLong, notional: decimal.NewFromInt(200_001),
			want: DecisionRejected, wantReason: ReasonInsufficientBuyingPower, wantMargin: decimal.RequireFromString("100000.5"), wantBP: decimal.NewFromInt(200_000),
		},
		{
			name: "reg t short", profile: domain.MarginProfileRegT, multiplier: decimal.NewFromInt(2),
			direction: ExposureIncreaseShort, notional: decimal.NewFromInt(60_000),
			want: DecisionAdmitted, wantReason: ReasonAdmitted, wantMargin: decimal.NewFromInt(90_000), wantBP: decimal.RequireFromString("66666.666666666666"),
		},
		{
			name: "portfolio long", profile: domain.MarginProfilePortfolio, multiplier: decimal.NewFromInt(6),
			direction: ExposureIncreaseLong, notional: decimal.NewFromInt(500_000),
			want: DecisionAdmitted, wantReason: ReasonAdmitted, wantMargin: decimal.NewFromInt(75_000), wantBP: decimal.NewFromInt(600_000),
		},
		{
			name: "portfolio gross breach", profile: domain.MarginProfilePortfolio, multiplier: decimal.NewFromInt(6),
			direction: ExposureIncreaseLong, notional: decimal.NewFromInt(620_000),
			want: DecisionRejected, wantReason: ReasonGrossExposureBreach, wantMargin: decimal.NewFromInt(93_000), wantBP: decimal.NewFromInt(600_000),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCapitalStateFixture(t, test.profile, test.multiplier, nil)
			state, err := StateFromProjection(fixture.account, fixture.binding, fixture.policy, fixture.projection, fixture.instruments)
			if err != nil {
				t.Fatal(err)
			}
			assessment, err := Assess(AssessmentInput{
				Account: fixture.account, Binding: fixture.binding, Policy: fixture.policy, State: state,
				Instrument: *capitalTestInstrument(t, instrument.AssetClassEquity), Currency: "USD",
				ScenarioID: "boundary-case", Direction: test.direction, ProposedNotional: test.notional,
			})
			if err != nil {
				t.Fatal(err)
			}
			if assessment.Decision != test.want || assessment.Reason != test.wantReason ||
				!assessment.RequestedInitialMargin.Equal(test.wantMargin) ||
				!assessment.AvailableBuyingPower.Equal(test.wantBP) {
				t.Fatalf("assessment = %+v", assessment)
			}
			if err := assessment.Validate(fixture.account, fixture.binding, fixture.policy, state); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAssessMaintenanceDeficiencyBlocksNewRiskButAllowsReduction(t *testing.T) {
	fixture := newCapitalStateFixtureForMode(
		t, domain.AccountEnvironmentPaperScored, decimal.NewFromInt(100_000),
		domain.MarginProfileRegT, decimal.NewFromInt(2), decimal.NewFromInt(-400_000),
		[]capitalPosition{{assetClass: instrument.AssetClassEquity, quantity: "5000", marketValue: "500000"}},
	)
	state, err := StateFromProjection(fixture.account, fixture.binding, fixture.policy, fixture.projection, fixture.instruments)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Equity().Equal(decimal.NewFromInt(100_000)) || !state.MaintenanceRequirement().Equal(decimal.NewFromInt(125_000)) {
		t.Fatalf("state = %+v", state)
	}
	entry, err := Assess(assessmentInputForFixture(t, fixture, state, ExposureIncreaseLong, decimal.NewFromInt(1)))
	if err != nil {
		t.Fatal(err)
	}
	if entry.Decision != DecisionRejected || entry.Reason != ReasonMaintenanceBreach {
		t.Fatalf("entry = %+v", entry)
	}
	reduction, err := Assess(assessmentInputForFixture(t, fixture, state, ExposureReduceLong, decimal.NewFromInt(100_000)))
	if err != nil {
		t.Fatal(err)
	}
	if reduction.Decision != DecisionAdmitted || reduction.Reason != ReasonAdmitted ||
		!reduction.PostGrossExposure.Equal(decimal.NewFromInt(400_000)) ||
		!reduction.PostMaintenanceRequirement.Equal(decimal.NewFromInt(100_000)) {
		t.Fatalf("reduction = %+v", reduction)
	}
}

func TestAssessStressUnlimitedIsSyntheticAndUnbounded(t *testing.T) {
	fixture := newCapitalStateFixtureForMode(
		t, domain.AccountEnvironmentPaperStress, decimal.NewFromInt(5_000_000),
		domain.MarginProfileStressUnlimited, decimal.Zero, decimal.Zero, nil,
	)
	state, err := StateFromProjection(fixture.account, fixture.binding, fixture.policy, fixture.projection, fixture.instruments)
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := Assess(assessmentInputForFixture(
		t, fixture, state, ExposureIncreaseLong, decimal.RequireFromString("99999999999999999999999999"),
	))
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Decision != DecisionAdmitted || assessment.Reason != ReasonStressUnbounded || !assessment.Unbounded ||
		!assessment.RequestedInitialMargin.IsZero() || assessment.PromotionEligible() {
		t.Fatalf("stress assessment = %+v", assessment)
	}
}

func TestAssessRejectsMalformedContextWithoutEvidence(t *testing.T) {
	fixture := newCapitalStateFixture(t, domain.MarginProfileRegT, decimal.NewFromInt(2), nil)
	state, err := StateFromProjection(fixture.account, fixture.binding, fixture.policy, fixture.projection, fixture.instruments)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*AssessmentInput){
		"scenario":      func(input *AssessmentInput) { input.ScenarioID = "" },
		"currency":      func(input *AssessmentInput) { input.Currency = "EUR" },
		"direction":     func(input *AssessmentInput) { input.Direction = ExposureDirection("unknown") },
		"zero notional": func(input *AssessmentInput) { input.ProposedNotional = decimal.Zero },
		"negative":      func(input *AssessmentInput) { input.ProposedNotional = decimal.NewFromInt(-1) },
		"over scale":    func(input *AssessmentInput) { input.ProposedNotional = decimal.RequireFromString("1.0000000000001") },
		"nil state":     func(input *AssessmentInput) { input.State = nil },
		"wrong account": func(input *AssessmentInput) {
			input.Account.ID = bindingTestAccount(t, domain.AccountEnvironmentPaperScored, decimal.NewFromInt(100_000), domain.MarginProfileRegT, decimal.NewFromInt(2)).ID
		},
		"unsupported": func(input *AssessmentInput) {
			input.Instrument.AssetClass = instrument.AssetClassCryptoSpot
		},
		"instrument EUR": func(input *AssessmentInput) { input.Instrument.Currency = "EUR" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input := assessmentInputForFixture(t, fixture, state, ExposureIncreaseLong, decimal.NewFromInt(1))
			mutate(&input)
			if assessment, err := Assess(input); err == nil || assessment != nil {
				t.Fatalf("Assess = %+v/%v", assessment, err)
			}
		})
	}

	positionFixture := newCapitalStateFixture(t, domain.MarginProfileRegT, decimal.NewFromInt(2), []capitalPosition{
		{assetClass: instrument.AssetClassEquity, quantity: "10", marketValue: "1000"},
	})
	positionState, err := StateFromProjection(positionFixture.account, positionFixture.binding, positionFixture.policy, positionFixture.projection, positionFixture.instruments)
	if err != nil {
		t.Fatal(err)
	}
	for name, direction := range map[string]ExposureDirection{
		"too much long reduction":   ExposureReduceLong,
		"short reduction from zero": ExposureReduceShort,
	} {
		t.Run(name, func(t *testing.T) {
			input := assessmentInputForFixture(t, positionFixture, positionState, direction, decimal.NewFromInt(1_001))
			if assessment, err := Assess(input); err == nil || assessment != nil {
				t.Fatalf("Assess = %+v/%v", assessment, err)
			}
		})
	}
}

func TestAssessmentCanonicalHashChangesWithOneCentAndDefendsBytes(t *testing.T) {
	fixture := newCapitalStateFixture(t, domain.MarginProfileRegT, decimal.NewFromInt(2), nil)
	state, err := StateFromProjection(fixture.account, fixture.binding, fixture.policy, fixture.projection, fixture.instruments)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Assess(assessmentInputForFixture(t, fixture, state, ExposureIncreaseLong, decimal.RequireFromString("100.00")))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Assess(assessmentInputForFixture(t, fixture, state, ExposureIncreaseLong, decimal.RequireFromString("100.01")))
	if err != nil {
		t.Fatal(err)
	}
	if first.Hash() == second.Hash() || bytes.Equal(first.CanonicalBytes(), second.CanonicalBytes()) || first.ID == second.ID {
		t.Fatal("changed cents shared assessment evidence")
	}
	cloned := first.CanonicalBytes()
	cloned[0] = '['
	if bytes.Equal(cloned, first.CanonicalBytes()) {
		t.Fatal("CanonicalBytes exposed mutable storage")
	}
}

func assessmentInputForFixture(
	t *testing.T,
	fixture capitalStateFixture,
	state *State,
	direction ExposureDirection,
	notional decimal.Decimal,
) AssessmentInput {
	t.Helper()
	return AssessmentInput{
		Account: fixture.account, Binding: fixture.binding, Policy: fixture.policy, State: state,
		Instrument: *capitalTestInstrument(t, instrument.AssetClassEquity), Currency: "USD",
		ScenarioID: "capital-assessment", Direction: direction, ProposedNotional: notional,
	}
}
