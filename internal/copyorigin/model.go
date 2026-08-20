// Package copyorigin owns immutable copy-subscription rebalance attribution.
package copyorigin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const SchemaV1 = "copy-origin-rebalance-v1"

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type intentCanonical struct {
	ID                  string `json:"id"`
	InstrumentKey       string `json:"instrument_key"`
	SourceObservationID string `json:"source_observation_id"`
}

type runCanonical struct {
	Schema              string            `json:"schema"`
	State               string            `json:"state"`
	SubscriptionID      string            `json:"subscription_id"`
	OriginType          string            `json:"origin_type"`
	OriginID            string            `json:"origin_id"`
	SourceObservationID string            `json:"source_observation_id"`
	CalculationVersion  int               `json:"calculation_version"`
	Intents             []intentCanonical `json:"intents"`
}

type Run struct {
	canonical runCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewRun(subscription domain.CopySubscription, intents []domain.CopyTradeIntent) (*Run, error) {
	if subscription.ID == uuid.Nil || subscription.OriginType != "copy_subscription" || subscription.OriginID != subscription.ID || len(intents) == 0 {
		return nil, fmt.Errorf("copy origin run requires an exact subscription and intents")
	}
	values := append([]domain.CopyTradeIntent(nil), intents...)
	sort.Slice(values, func(i, j int) bool { return values[i].InstrumentKey < values[j].InstrumentKey })
	canonical := runCanonical{Schema: SchemaV1, State: "prepared", SubscriptionID: subscription.ID.String(), OriginType: subscription.OriginType, OriginID: subscription.OriginID.String(), Intents: make([]intentCanonical, len(values))}
	seen := map[string]bool{}
	for i, intent := range values {
		if intent.ID == uuid.Nil || intent.SubscriptionID != subscription.ID || intent.OriginType != subscription.OriginType || intent.OriginID != subscription.OriginID || intent.SourceObservationID == uuid.Nil || intent.InstrumentKey == "" || seen[intent.InstrumentKey] {
			return nil, fmt.Errorf("copy origin run intent attribution is invalid")
		}
		if i == 0 {
			canonical.SourceObservationID = intent.SourceObservationID.String()
			canonical.CalculationVersion = intent.CalculationVersion
		} else if canonical.SourceObservationID != intent.SourceObservationID.String() || canonical.CalculationVersion != intent.CalculationVersion {
			return nil, fmt.Errorf("copy origin run intents do not share source and calculation version")
		}
		seen[intent.InstrumentKey] = true
		canonical.Intents[i] = intentCanonical{intent.ID.String(), intent.InstrumentKey, intent.SourceObservationID.String()}
	}
	if canonical.CalculationVersion < 1 {
		return nil, fmt.Errorf("copy origin run calculation version is invalid")
	}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	id := economicid.DeterministicUUID("copy-origin-rebalance", SchemaV1+"@sha256:"+digest)
	return &Run{canonical, encoded, digest, id}, nil
}

func FromCanonical(id uuid.UUID, digest string, raw []byte) (*Run, error) {
	var canonical runCanonical
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil || canonical.Schema != SchemaV1 || canonical.State != "prepared" {
		return nil, fmt.Errorf("copy origin run envelope is invalid")
	}
	subscriptionID, subscriptionErr := uuid.Parse(canonical.SubscriptionID)
	originID, originErr := uuid.Parse(canonical.OriginID)
	if subscriptionErr != nil || originErr != nil || subscriptionID == uuid.Nil || originID != subscriptionID || canonical.OriginType != "copy_subscription" || canonical.CalculationVersion < 1 || len(canonical.Intents) == 0 {
		return nil, fmt.Errorf("copy origin run attribution is invalid")
	}
	last := ""
	for _, intent := range canonical.Intents {
		intentID, intentErr := uuid.Parse(intent.ID)
		sourceID, sourceErr := uuid.Parse(intent.SourceObservationID)
		if intentErr != nil || sourceErr != nil || intentID == uuid.Nil || sourceID == uuid.Nil || intent.SourceObservationID != canonical.SourceObservationID || intent.InstrumentKey == "" || intent.InstrumentKey <= last {
			return nil, fmt.Errorf("copy origin run intent graph is invalid")
		}
		last = intent.InstrumentKey
	}
	encoded, _ := json.Marshal(canonical)
	value := &Run{canonical, encoded, digest, economicid.DeterministicUUID("copy-origin-rebalance", SchemaV1+"@sha256:"+digest)}
	if value.id != id || !bytes.Equal(encoded, raw) {
		return nil, fmt.Errorf("copy origin run identity does not reconstruct")
	}
	return value, nil
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

func hash(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
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
