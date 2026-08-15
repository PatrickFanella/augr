package marketdata

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
)

func TestNewQuoteSnapshotPreservesExactTopOfBook(t *testing.T) {
	exchangeAt := time.Date(2026, 8, 15, 14, 0, 0, 123456789, time.UTC)
	receivedAt := exchangeAt.Add(25 * time.Millisecond)
	availableAt := receivedAt.Add(time.Millisecond)
	bid := decimal.RequireFromString("100.125")
	ask := decimal.RequireFromString("100.375")

	got, err := NewQuoteSnapshot(QuoteSnapshotInput{
		InstrumentID:         uuid.New(),
		Provider:             " Polygon ",
		Venue:                " XNAS ",
		Source:               "sip",
		ObservationNamespace: " stocks/sip/a ",
		ObservationID:        " quote-42 ",
		ExchangeAt:           &exchangeAt,
		ReceivedAt:           receivedAt,
		AvailableAt:          &availableAt,
		Bid:                  &bid,
		Ask:                  &ask,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "polygon" || got.Venue != "xnas" ||
		got.ObservationNamespace != "stocks/sip/a" || got.ObservationID != "quote-42" ||
		got.Bid == nil || !got.Bid.Equal(bid) || got.Ask == nil || !got.Ask.Equal(ask) ||
		!got.ReceivedAt.Equal(receivedAt.Truncate(time.Microsecond)) {
		t.Fatalf("unexpected snapshot: %+v", got)
	}
}

func TestQuoteSnapshotRequiresAttributableObservationIdentity(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*QuoteSnapshotInput)
	}{
		{name: "instrument", modify: func(input *QuoteSnapshotInput) { input.InstrumentID = uuid.Nil }},
		{name: "provider", modify: func(input *QuoteSnapshotInput) { input.Provider = " " }},
		{name: "venue", modify: func(input *QuoteSnapshotInput) { input.Venue = " " }},
		{name: "namespace", modify: func(input *QuoteSnapshotInput) { input.ObservationNamespace = " " }},
		{name: "observation ID", modify: func(input *QuoteSnapshotInput) { input.ObservationID = " " }},
		{name: "receive time", modify: func(input *QuoteSnapshotInput) { input.ReceivedAt = time.Time{} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validQuoteSnapshotInput()
			test.modify(&input)
			if _, err := NewQuoteSnapshot(input); err == nil {
				t.Fatal("NewQuoteSnapshot() unexpectedly accepted missing identity")
			}
		})
	}
}

func TestQuoteSnapshotAllowsMissingExecutableFieldsWithoutZeroSentinels(t *testing.T) {
	got, err := NewQuoteSnapshot(validQuoteSnapshotInput())
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "" || got.VenueContractID != nil || got.ExchangeAt != nil || got.AvailableAt != nil ||
		got.Bid != nil || got.Ask != nil || got.Last != nil || got.Mark != nil || len(got.Depth) != 0 {
		t.Fatalf("optional fields were invented: %+v", got)
	}
}

func TestQuoteSnapshotDistinguishesPresentZeroFromMissingPrice(t *testing.T) {
	zero := decimal.Zero
	input := validQuoteSnapshotInput()
	input.Bid = &zero
	input.Ask = &zero
	got, err := NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bid == nil || got.Ask == nil || !got.Bid.IsZero() || !got.Ask.IsZero() {
		t.Fatalf("present zero quote became missing: bid=%v ask=%v", got.Bid, got.Ask)
	}
}

func TestQuoteSnapshotRejectsCrossedTopOfBook(t *testing.T) {
	bid := decimal.RequireFromString("10.01")
	ask := decimal.RequireFromString("10.00")
	input := validQuoteSnapshotInput()
	input.Bid = &bid
	input.Ask = &ask
	if _, err := NewQuoteSnapshot(input); err == nil {
		t.Fatal("NewQuoteSnapshot() accepted crossed quote")
	}
}

func TestQuoteSnapshotRejectsSizeWithoutMatchingPrice(t *testing.T) {
	size := decimal.NewFromInt(1)
	for _, side := range []string{"bid", "ask"} {
		t.Run(side, func(t *testing.T) {
			input := validQuoteSnapshotInput()
			if side == "bid" {
				input.BidSize = &size
			} else {
				input.AskSize = &size
			}
			if _, err := NewQuoteSnapshot(input); err == nil {
				t.Fatal("NewQuoteSnapshot() accepted size without price")
			}
		})
	}
}

func TestQuoteSnapshotRejectsInvalidObservationTimes(t *testing.T) {
	receivedAt := validQuoteSnapshotInput().ReceivedAt
	tests := []struct {
		name   string
		modify func(*QuoteSnapshotInput)
	}{
		{name: "exchange after receive", modify: func(input *QuoteSnapshotInput) {
			exchangeAt := receivedAt.Add(time.Microsecond)
			input.ExchangeAt = &exchangeAt
		}},
		{name: "availability before receive", modify: func(input *QuoteSnapshotInput) {
			availableAt := receivedAt.Add(-time.Microsecond)
			input.AvailableAt = &availableAt
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validQuoteSnapshotInput()
			test.modify(&input)
			if _, err := NewQuoteSnapshot(input); err == nil {
				t.Fatal("NewQuoteSnapshot() accepted impossible timestamp ordering")
			}
		})
	}
}

func TestQuoteSnapshotRejectsNegativeSourceSequence(t *testing.T) {
	sequence := int64(-1)
	input := validQuoteSnapshotInput()
	input.SourceSequence = &sequence
	if _, err := NewQuoteSnapshot(input); err == nil {
		t.Fatal("NewQuoteSnapshot() accepted negative source sequence")
	}
}

func TestQuoteSnapshotRejectsNumericBoundsBeyondSchema(t *testing.T) {
	tests := []struct {
		name  string
		value decimal.Decimal
	}{
		{name: "negative", value: decimal.RequireFromString("-0.01")},
		{name: "scale", value: decimal.RequireFromString("1.0000000000001")},
		{name: "magnitude", value: decimal.RequireFromString("100000000000000000000000000")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validQuoteSnapshotInput()
			input.Bid = &test.value
			if _, err := NewQuoteSnapshot(input); err == nil {
				t.Fatal("NewQuoteSnapshot() accepted out-of-contract decimal")
			}
		})
	}
}

func TestQuoteSnapshotRejectsNonObjectMetadata(t *testing.T) {
	input := validQuoteSnapshotInput()
	input.Metadata = json.RawMessage(`[]`)
	if _, err := NewQuoteSnapshot(input); err == nil {
		t.Fatal("NewQuoteSnapshot() accepted non-object metadata")
	}
}

func TestQuoteSnapshotNormalizesStatusesAndRejectsUnnormalizedManualFields(t *testing.T) {
	input := validQuoteSnapshotInput()
	input.MarketStatus = " OPEN "
	input.SessionStatus = " Regular "
	got, err := NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.MarketStatus != "open" || got.SessionStatus != "regular" {
		t.Fatalf("statuses = %q/%q, want open/regular", got.MarketStatus, got.SessionStatus)
	}
	got.Provider = strings.ToUpper(got.Provider)
	if err := got.Validate(); err == nil {
		t.Fatal("Validate() accepted manually denormalized provider")
	}
}

func TestQuoteSnapshotRejectsBidDepthThatDoesNotStrictlyDescend(t *testing.T) {
	input := validQuoteSnapshotInput()
	input.Bids = []DepthLevelInput{
		{Price: decimal.RequireFromString("10.00"), Size: decimal.NewFromInt(2)},
		{Price: decimal.RequireFromString("10.01"), Size: decimal.NewFromInt(3)},
	}
	if _, err := NewQuoteSnapshot(input); err == nil {
		t.Fatal("NewQuoteSnapshot() accepted ascending bid depth")
	}
}

func TestQuoteSnapshotAcceptsOrderedBookMatchingTopOfBook(t *testing.T) {
	bid := decimal.RequireFromString("10.00")
	bidSize := decimal.NewFromInt(2)
	ask := decimal.RequireFromString("10.02")
	askSize := decimal.NewFromInt(4)
	input := validQuoteSnapshotInput()
	input.Bid, input.BidSize = &bid, &bidSize
	input.Ask, input.AskSize = &ask, &askSize
	input.Bids = []DepthLevelInput{
		{Price: bid, Size: bidSize},
		{Price: decimal.RequireFromString("9.99"), Size: decimal.NewFromInt(3)},
	}
	input.Asks = []DepthLevelInput{
		{Price: ask, Size: askSize},
		{Price: decimal.RequireFromString("10.03"), Size: decimal.NewFromInt(5)},
	}
	got, err := NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Depth) != 4 || got.Depth[0].Side != DepthSideBid || got.Depth[0].Level != 0 ||
		got.Depth[2].Side != DepthSideAsk || got.Depth[2].Level != 0 {
		t.Fatalf("canonical depth = %+v", got.Depth)
	}
}

func TestQuoteSnapshotRejectsInvalidDepthShapes(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*QuoteSnapshotInput)
	}{
		{name: "descending asks", modify: func(input *QuoteSnapshotInput) {
			input.Asks = []DepthLevelInput{
				{Price: decimal.RequireFromString("10.02"), Size: decimal.NewFromInt(1)},
				{Price: decimal.RequireFromString("10.01"), Size: decimal.NewFromInt(1)},
			}
		}},
		{name: "negative depth", modify: func(input *QuoteSnapshotInput) {
			input.Bids = []DepthLevelInput{{Price: decimal.NewFromInt(10), Size: decimal.NewFromInt(-1)}}
		}},
		{name: "crossed depth without quote", modify: func(input *QuoteSnapshotInput) {
			input.Bids = []DepthLevelInput{{Price: decimal.RequireFromString("10.03"), Size: decimal.NewFromInt(1)}}
			input.Asks = []DepthLevelInput{{Price: decimal.RequireFromString("10.02"), Size: decimal.NewFromInt(1)}}
		}},
		{name: "top price mismatch", modify: func(input *QuoteSnapshotInput) {
			bid := decimal.NewFromInt(10)
			input.Bid = &bid
			input.Bids = []DepthLevelInput{{Price: decimal.RequireFromString("9.99"), Size: decimal.NewFromInt(1)}}
		}},
		{name: "top size mismatch", modify: func(input *QuoteSnapshotInput) {
			bid := decimal.NewFromInt(10)
			size := decimal.NewFromInt(2)
			input.Bid, input.BidSize = &bid, &size
			input.Bids = []DepthLevelInput{{Price: bid, Size: decimal.NewFromInt(1)}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validQuoteSnapshotInput()
			test.modify(&input)
			if _, err := NewQuoteSnapshot(input); err == nil {
				t.Fatal("NewQuoteSnapshot() accepted invalid depth")
			}
		})
	}
}

func TestQuoteSnapshotRejectsMoreThanMaximumDepthLevels(t *testing.T) {
	input := validQuoteSnapshotInput()
	input.Bids = make([]DepthLevelInput, MaxDepthLevelsPerSide+1)
	for index := range input.Bids {
		input.Bids[index] = DepthLevelInput{
			Price: decimal.NewFromInt(int64(MaxDepthLevelsPerSide + 1 - index)),
			Size:  decimal.NewFromInt(1),
		}
	}
	if _, err := NewQuoteSnapshot(input); err == nil {
		t.Fatal("NewQuoteSnapshot() accepted depth above safety ceiling")
	}
}

func TestNewQuoteSelectorNormalizesPointInTimeIdentity(t *testing.T) {
	asOf := time.Date(2026, 8, 15, 15, 30, 0, 123456789, time.FixedZone("test", -5*60*60))
	selector, err := NewQuoteSelector(uuid.New(), " Provider ", " XNAS ", " feed/a ", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if selector.Provider != "provider" || selector.Venue != "xnas" ||
		selector.ObservationNamespace != "feed/a" || selector.AsOf.Location() != time.UTC ||
		selector.AsOf.Nanosecond()%1_000 != 0 {
		t.Fatalf("selector = %+v", selector)
	}
}

func TestQuoteAssessmentFailsClosedForRequiredSource(t *testing.T) {
	input := executableQuoteSnapshotInput()
	input.Source = ""
	snapshot, err := NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	_, err = snapshot.Assess(input.ReceivedAt.Add(time.Second), QuoteRequirements{RequireSource: true})
	var assessmentErr *AssessmentError
	if !errors.As(err, &assessmentErr) || assessmentErr.Code != AssessmentMissingSource {
		t.Fatalf("Assess() error = %v, want %s", err, AssessmentMissingSource)
	}
}

func TestQuoteAssessmentFailsClosedForRequiredVenueContract(t *testing.T) {
	input := executableQuoteSnapshotInput()
	input.VenueContractID = nil
	snapshot, err := NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	_, err = snapshot.Assess(input.ReceivedAt.Add(time.Second), QuoteRequirements{RequireVenueContract: true})
	assertAssessmentCode(t, err, AssessmentMissingVenueContract)
}

func TestQuoteAssessmentFailsClosedWhenAvailabilityIsUnknown(t *testing.T) {
	input := executableQuoteSnapshotInput()
	input.AvailableAt = nil
	snapshot, err := NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	_, err = snapshot.Assess(input.ReceivedAt.Add(time.Second), QuoteRequirements{})
	assertAssessmentCode(t, err, AssessmentMissingAvailability)
}

func TestQuoteAssessmentFailsClosedForRequiredBidAndAsk(t *testing.T) {
	for _, side := range []string{"bid", "ask"} {
		t.Run(side, func(t *testing.T) {
			input := executableQuoteSnapshotInput()
			requirements := QuoteRequirements{}
			if side == "bid" {
				input.Bid, input.Bids = nil, nil
				requirements.RequireBid = true
			} else {
				input.Ask, input.Asks = nil, nil
				requirements.RequireAsk = true
			}
			snapshot, err := NewQuoteSnapshot(input)
			if err != nil {
				t.Fatal(err)
			}
			_, err = snapshot.Assess(input.ReceivedAt.Add(time.Second), requirements)
			if side == "bid" {
				assertAssessmentCode(t, err, AssessmentMissingBid)
			} else {
				assertAssessmentCode(t, err, AssessmentMissingAsk)
			}
		})
	}
}

func TestQuoteAssessmentFailsClosedWhenAgeCannotBeCalculated(t *testing.T) {
	input := executableQuoteSnapshotInput()
	input.ExchangeAt = nil
	snapshot, err := NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	_, err = snapshot.Assess(input.ReceivedAt.Add(time.Second), QuoteRequirements{MaxAge: time.Second})
	assertAssessmentCode(t, err, AssessmentMissingExchangeTime)
}

func TestQuoteAssessmentRejectsStaleQuoteAtIntentTime(t *testing.T) {
	input := executableQuoteSnapshotInput()
	snapshot, err := NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	_, err = snapshot.Assess(input.ReceivedAt.Add(2*time.Second), QuoteRequirements{MaxAge: time.Second})
	assertAssessmentCode(t, err, AssessmentStaleQuote)
}

func TestQuoteAssessmentRejectsObservationAvailableAfterDecision(t *testing.T) {
	input := executableQuoteSnapshotInput()
	snapshot, err := NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	_, err = snapshot.Assess(input.ReceivedAt, QuoteRequirements{})
	assertAssessmentCode(t, err, AssessmentFutureObservation)
}

func TestQuoteAssessmentRequiresRequestedDepthSide(t *testing.T) {
	for _, side := range []DepthSide{DepthSideBid, DepthSideAsk} {
		t.Run(string(side), func(t *testing.T) {
			input := executableQuoteSnapshotInput()
			requirements := QuoteRequirements{}
			if side == DepthSideBid {
				input.Bids = nil
				requirements.RequireBidDepth = true
			} else {
				input.Asks = nil
				requirements.RequireAskDepth = true
			}
			snapshot, err := NewQuoteSnapshot(input)
			if err != nil {
				t.Fatal(err)
			}
			_, err = snapshot.Assess(input.ReceivedAt.Add(time.Second), requirements)
			if side == DepthSideBid {
				assertAssessmentCode(t, err, AssessmentMissingBidDepth)
			} else {
				assertAssessmentCode(t, err, AssessmentMissingAskDepth)
			}
		})
	}
}

func TestQuoteAssessmentRequiresRequestedMarketAndSessionStatus(t *testing.T) {
	tests := []struct {
		name         string
		modify       func(*QuoteSnapshotInput)
		requirements QuoteRequirements
		want         AssessmentCode
	}{
		{name: "market", modify: func(input *QuoteSnapshotInput) { input.MarketStatus = "" }, requirements: QuoteRequirements{RequireMarketStatus: true}, want: AssessmentMissingMarketStatus},
		{name: "session", modify: func(input *QuoteSnapshotInput) { input.SessionStatus = "" }, requirements: QuoteRequirements{RequireSessionStatus: true}, want: AssessmentMissingSessionStatus},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := executableQuoteSnapshotInput()
			test.modify(&input)
			snapshot, err := NewQuoteSnapshot(input)
			if err != nil {
				t.Fatal(err)
			}
			_, err = snapshot.Assess(input.ReceivedAt.Add(time.Second), test.requirements)
			assertAssessmentCode(t, err, test.want)
		})
	}
}

func TestQuoteAssessmentStatusAllowlistImplicitlyRequiresStatus(t *testing.T) {
	tests := []struct {
		name         string
		modify       func(*QuoteSnapshotInput)
		requirements QuoteRequirements
		want         AssessmentCode
	}{
		{name: "market", modify: func(input *QuoteSnapshotInput) { input.MarketStatus = "" }, requirements: QuoteRequirements{AllowedMarketStatuses: []string{"open"}}, want: AssessmentMissingMarketStatus},
		{name: "session", modify: func(input *QuoteSnapshotInput) { input.SessionStatus = "" }, requirements: QuoteRequirements{AllowedSessionStatuses: []string{"regular"}}, want: AssessmentMissingSessionStatus},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := executableQuoteSnapshotInput()
			test.modify(&input)
			snapshot, err := NewQuoteSnapshot(input)
			if err != nil {
				t.Fatal(err)
			}
			_, err = snapshot.Assess(input.ReceivedAt.Add(time.Second), test.requirements)
			assertAssessmentCode(t, err, test.want)
		})
	}
}

func TestQuoteAssessmentRejectsDisallowedMarketOrSessionStatus(t *testing.T) {
	input := executableQuoteSnapshotInput()
	snapshot, err := NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	_, err = snapshot.Assess(input.ReceivedAt.Add(time.Second), QuoteRequirements{AllowedMarketStatuses: []string{"closed"}})
	assertAssessmentCode(t, err, AssessmentMarketNotExecutable)
	_, err = snapshot.Assess(input.ReceivedAt.Add(time.Second), QuoteRequirements{AllowedSessionStatuses: []string{"after_hours"}})
	assertAssessmentCode(t, err, AssessmentSessionNotExecutable)
}

func TestQuoteAssessmentKeepsMissingSpreadNilAndLockedSpreadPresent(t *testing.T) {
	input := executableQuoteSnapshotInput()
	input.Bid, input.Bids = nil, nil
	snapshot, err := NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := snapshot.Assess(input.ReceivedAt.Add(time.Second), QuoteRequirements{})
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Spread != nil {
		t.Fatalf("missing spread = %s, want nil", assessment.Spread)
	}

	zero := decimal.Zero
	lockedInput := executableQuoteSnapshotInput()
	lockedInput.Bid, lockedInput.Ask = &zero, &zero
	lockedInput.Bids = []DepthLevelInput{{Price: zero, Size: decimal.NewFromInt(1)}}
	lockedInput.Asks = []DepthLevelInput{{Price: zero, Size: decimal.NewFromInt(1)}}
	locked, err := NewQuoteSnapshot(lockedInput)
	if err != nil {
		t.Fatal(err)
	}
	assessment, err = locked.Assess(lockedInput.ReceivedAt.Add(time.Second), QuoteRequirements{})
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Spread == nil || !assessment.Spread.IsZero() {
		t.Fatalf("locked spread = %v, want present zero", assessment.Spread)
	}
}

func TestQuoteAssessmentReturnsPointInTimeLatencyEvidence(t *testing.T) {
	input := executableQuoteSnapshotInput()
	snapshot, err := NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	asOf := input.ReceivedAt.Add(time.Second)
	assessment, err := snapshot.Assess(asOf, QuoteRequirements{
		RequireSource: true, RequireVenueContract: true,
		RequireBid: true, RequireAsk: true,
		RequireBidDepth: true, RequireAskDepth: true,
		AllowedMarketStatuses: []string{" OPEN "}, AllowedSessionStatuses: []string{"regular"},
		MaxAge: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if assessment.ExchangeAge == nil || *assessment.ExchangeAge != 1025*time.Millisecond ||
		assessment.TransportLatency == nil || *assessment.TransportLatency != 25*time.Millisecond ||
		assessment.ValidationLatency == nil || *assessment.ValidationLatency != time.Millisecond ||
		assessment.AvailabilityAge != 999*time.Millisecond || assessment.Spread == nil ||
		!assessment.Spread.Equal(decimal.RequireFromString("0.02")) {
		t.Fatalf("assessment = %+v", assessment)
	}
}

func TestQuoteSnapshotValidatesExactVenueMechanics(t *testing.T) {
	_, contract, input := executableReferenceFixture(t)
	snapshot, err := NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.ValidateAgainstVenueContract(*contract); err != nil {
		t.Fatalf("ValidateAgainstVenueContract() error = %v", err)
	}
}

func TestQuoteSnapshotRejectsOffTickExecutablePrices(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*QuoteSnapshotInput)
	}{
		{name: "top of book", modify: func(input *QuoteSnapshotInput) {
			bid := decimal.RequireFromString("10.005")
			input.Bid = &bid
			input.Bids[0].Price = bid
		}},
		{name: "depth", modify: func(input *QuoteSnapshotInput) {
			input.Bids = append(input.Bids, DepthLevelInput{
				Price: decimal.RequireFromString("9.995"),
				Size:  decimal.NewFromInt(1),
			})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, contract, input := executableReferenceFixture(t)
			test.modify(&input)
			snapshot, err := NewQuoteSnapshot(input)
			if err != nil {
				t.Fatal(err)
			}
			assertAssessmentCode(t, snapshot.ValidateAgainstVenueContract(*contract), AssessmentInvalidPriceTick)
		})
	}
}

func TestQuoteSnapshotRejectsOffLotExecutableSizes(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*QuoteSnapshotInput)
	}{
		{name: "top of book", modify: func(input *QuoteSnapshotInput) {
			size := decimal.RequireFromString("5.5")
			input.BidSize = &size
			input.Bids[0].Size = size
		}},
		{name: "depth", modify: func(input *QuoteSnapshotInput) {
			input.Bids = append(input.Bids, DepthLevelInput{
				Price: decimal.RequireFromString("9.99"),
				Size:  decimal.RequireFromString("1.5"),
			})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, contract, input := executableReferenceFixture(t)
			test.modify(&input)
			snapshot, err := NewQuoteSnapshot(input)
			if err != nil {
				t.Fatal(err)
			}
			assertAssessmentCode(t, snapshot.ValidateAgainstVenueContract(*contract), AssessmentInvalidLotSize)
		})
	}
}

func TestQuoteSnapshotVenueMechanicsDoNotInventLastOrMarkExecutability(t *testing.T) {
	_, contract, input := executableReferenceFixture(t)
	last := decimal.RequireFromString("10.005")
	mark := decimal.RequireFromString("10.007")
	input.Last, input.Mark = &last, &mark
	snapshot, err := NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.ValidateAgainstVenueContract(*contract); err != nil {
		t.Fatalf("non-executable last/mark values should remain evidence: %v", err)
	}
}

func TestQuoteSnapshotRejectsMismatchedVenueContractReference(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*QuoteSnapshotInput, *instrument.VenueContract)
		want   AssessmentCode
	}{
		{name: "missing binding", modify: func(input *QuoteSnapshotInput, _ *instrument.VenueContract) {
			input.VenueContractID = nil
		}, want: AssessmentMissingVenueContract},
		{name: "different contract ID", modify: func(input *QuoteSnapshotInput, _ *instrument.VenueContract) {
			otherContractID := uuid.New()
			input.VenueContractID = &otherContractID
		}, want: AssessmentVenueContractMismatch},
		{name: "different instrument", modify: func(_ *QuoteSnapshotInput, contract *instrument.VenueContract) {
			contract.InstrumentID = uuid.New()
		}, want: AssessmentVenueContractMismatch},
		{name: "different venue", modify: func(_ *QuoteSnapshotInput, contract *instrument.VenueContract) {
			contract.Venue = "other-venue"
		}, want: AssessmentVenueContractMismatch},
		{name: "outside effective window", modify: func(input *QuoteSnapshotInput, contract *instrument.VenueContract) {
			contract.ValidTo = input.ExchangeAt
		}, want: AssessmentVenueContractMismatch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, contract, input := executableReferenceFixture(t)
			test.modify(&input, contract)
			snapshot, err := NewQuoteSnapshot(input)
			if err != nil {
				t.Fatal(err)
			}
			assertAssessmentCode(t, snapshot.ValidateAgainstVenueContract(*contract), test.want)
		})
	}
}

func TestQuoteSnapshotExecutionAssessmentRequiresActiveMatchingInstrument(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*instrument.Instrument, time.Time)
		want   AssessmentCode
	}{
		{name: "inactive", modify: func(reference *instrument.Instrument, _ time.Time) {
			reference.Status = instrument.StatusInactive
		}, want: AssessmentInstrumentNotExecutable},
		{name: "different instrument", modify: func(reference *instrument.Instrument, _ time.Time) {
			reference.ID = uuid.New()
		}, want: AssessmentInstrumentMismatch},
		{name: "expired", modify: func(reference *instrument.Instrument, asOf time.Time) {
			reference.Expiration = &asOf
		}, want: AssessmentInstrumentNotExecutable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reference, contract, input := executableReferenceFixture(t)
			snapshot, err := NewQuoteSnapshot(input)
			if err != nil {
				t.Fatal(err)
			}
			asOf := input.AvailableAt.Add(time.Second)
			test.modify(reference, asOf)
			_, err = snapshot.AssessForExecution(asOf, QuoteRequirements{}, *reference, *contract)
			assertAssessmentCode(t, err, test.want)
		})
	}
}

func TestQuoteSnapshotExecutionAssessmentJoinsObservationAndReferenceChecks(t *testing.T) {
	reference, contract, input := executableReferenceFixture(t)
	snapshot, err := NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := snapshot.AssessForExecution(
		input.AvailableAt.Add(time.Second),
		QuoteRequirements{RequireBid: true, RequireAsk: true},
		*reference,
		*contract,
	)
	if err != nil {
		t.Fatalf("AssessForExecution() error = %v", err)
	}
	if assessment.SnapshotID != snapshot.ID || assessment.VenueContractID == nil ||
		*assessment.VenueContractID != contract.ID {
		t.Fatalf("execution assessment = %+v", assessment)
	}
}

func TestQuoteSnapshotExecutionAssessmentRequiresContractAtEvaluationTime(t *testing.T) {
	reference, contract, input := executableReferenceFixture(t)
	validTo := input.AvailableAt.Add(time.Second)
	contract.ValidTo = &validTo
	snapshot, err := NewQuoteSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}

	beforeValidTo := validTo.Add(-time.Microsecond)
	if _, err := snapshot.AssessForExecution(beforeValidTo, QuoteRequirements{}, *reference, *contract); err != nil {
		t.Fatalf("AssessForExecution(before valid-to) error = %v", err)
	}
	_, err = snapshot.AssessForExecution(validTo, QuoteRequirements{}, *reference, *contract)
	assertAssessmentCode(t, err, AssessmentVenueContractMismatch)
}

func assertAssessmentCode(t *testing.T, err error, want AssessmentCode) {
	t.Helper()
	var assessmentErr *AssessmentError
	if !errors.As(err, &assessmentErr) || assessmentErr.Code != want {
		t.Fatalf("assessment error = %v, want %s", err, want)
	}
}

func validQuoteSnapshotInput() QuoteSnapshotInput {
	return QuoteSnapshotInput{
		InstrumentID:         uuid.New(),
		Provider:             "test-provider",
		Venue:                "test-venue",
		ObservationNamespace: "test/feed/one",
		ObservationID:        uuid.NewString(),
		ReceivedAt:           time.Date(2026, 8, 15, 15, 0, 0, 0, time.UTC),
	}
}

func executableQuoteSnapshotInput() QuoteSnapshotInput {
	input := validQuoteSnapshotInput()
	exchangeAt := input.ReceivedAt.Add(-25 * time.Millisecond)
	availableAt := input.ReceivedAt.Add(time.Millisecond)
	bid := decimal.RequireFromString("10.00")
	ask := decimal.RequireFromString("10.02")
	input.Source = "test-feed"
	input.VenueContractID = pointerUUID(uuid.New())
	input.ExchangeAt = &exchangeAt
	input.AvailableAt = &availableAt
	input.Bid = &bid
	input.Ask = &ask
	input.MarketStatus = "open"
	input.SessionStatus = "regular"
	input.Bids = []DepthLevelInput{{Price: bid, Size: decimal.NewFromInt(5)}}
	input.Asks = []DepthLevelInput{{Price: ask, Size: decimal.NewFromInt(6)}}
	return input
}

func executableReferenceFixture(t *testing.T) (*instrument.Instrument, *instrument.VenueContract, QuoteSnapshotInput) {
	t.Helper()
	input := executableQuoteSnapshotInput()
	reference, err := instrument.NewInstrument(instrument.InstrumentInput{
		IdentityKey:      "figi:test:" + uuid.NewString(),
		AssetClass:       instrument.AssetClassEquity,
		PrimaryVenue:     input.Venue,
		Currency:         "USD",
		TickSize:         decimal.RequireFromString("0.01"),
		LotSize:          decimal.NewFromInt(1),
		Multiplier:       decimal.NewFromInt(1),
		SettlementMethod: instrument.SettlementPhysical,
		Status:           instrument.StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	input.InstrumentID = reference.ID
	validFrom := input.ExchangeAt.Add(-time.Hour)
	validTo := input.ExchangeAt.Add(time.Hour)
	contract, err := instrument.NewVenueContract(instrument.VenueContractInput{
		InstrumentID:     reference.ID,
		Venue:            input.Venue,
		ContractID:       "TEST-CONTRACT-" + uuid.NewString(),
		Currency:         "USD",
		TickSize:         decimal.RequireFromString("0.01"),
		LotSize:          decimal.NewFromInt(1),
		Multiplier:       decimal.NewFromInt(1),
		SettlementMethod: instrument.SettlementPhysical,
		ValidFrom:        validFrom,
		ValidTo:          &validTo,
	})
	if err != nil {
		t.Fatal(err)
	}
	input.VenueContractID = &contract.ID
	return reference, contract, input
}

func pointerUUID(value uuid.UUID) *uuid.UUID {
	return &value
}
