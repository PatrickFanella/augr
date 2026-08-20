package venuerecon

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
)

const (
	localSnapshotSchemaV1 = "venue-local-snapshot-v1"
	localSnapshotDomain   = "venue-reconciliation-local-snapshot"
)

// LocalFill retains the complete immutable OVR-203/205/103 linkage required to
// compare one local lifecycle fill with provider evidence.
type LocalFill struct {
	FillID                   uuid.UUID
	IntentID                 uuid.UUID
	OrderID                  uuid.UUID
	AccountID                uuid.UUID
	Provider                 venue.Provider
	Namespace                string
	SourceID                 string
	SourceRevision           string
	ObservationClass         lifecycle.ObservationClass
	ObservationDiscriminator string
	OriginalFillID           uuid.UUID
	OriginalSourceID         string
	ExternalOrderID          string
	ClientOrderID            string
	InstrumentID             uuid.UUID
	VenueContractID          uuid.UUID
	Side                     lifecycle.Side
	Quantity                 decimal.Decimal
	Price                    decimal.Decimal
	Fee                      decimal.Decimal
	Currency                 string
	SourceAt                 time.Time
	NormalizationID          uuid.UUID
	LedgerTransactionID      uuid.UUID
}

// LocalSnapshotIssue keeps excluded/incomplete lifecycle evidence visible.
type LocalSnapshotIssue struct {
	Reason              ReasonCode
	AccountID           uuid.UUID
	Provider            venue.Provider
	Namespace           string
	SourceID            string
	SourceAt            time.Time
	VenueContractID     uuid.UUID
	LedgerTransactionID uuid.UUID
	EvidenceID          uuid.UUID
}

// LocalSnapshotInput is returned as one unit by the transaction-owning reader.
// It deliberately has no caller-authored cash or position totals.
type LocalSnapshotInput struct {
	AccountID      uuid.UUID
	Provider       venue.Provider
	Namespace      string
	HorizonStart   time.Time
	HorizonEnd     time.Time
	Checkpoint     *ledger.ProjectionCheckpoint
	TransactionIDs []uuid.UUID
	Fills          []LocalFill
	Issues         []LocalSnapshotIssue
}

type LocalPosition struct {
	InstrumentID uuid.UUID `json:"instrument_id"`
	Quantity     string    `json:"quantity"`
}

type LocalFillEvidence struct {
	FillID                   string                     `json:"fill_id"`
	IntentID                 string                     `json:"intent_id"`
	OrderID                  string                     `json:"order_id"`
	SourceID                 string                     `json:"source_id"`
	SourceRevision           string                     `json:"source_revision"`
	ObservationClass         lifecycle.ObservationClass `json:"observation_class"`
	ObservationDiscriminator string                     `json:"observation_discriminator"`
	OriginalFillID           string                     `json:"original_fill_id"`
	OriginalSourceID         string                     `json:"original_source_id"`
	ExternalOrderID          string                     `json:"external_order_id"`
	ClientOrderID            string                     `json:"client_order_id"`
	InstrumentID             string                     `json:"instrument_id"`
	VenueContractID          string                     `json:"venue_contract_id"`
	Side                     lifecycle.Side             `json:"side"`
	Quantity                 string                     `json:"quantity"`
	Price                    string                     `json:"price"`
	Fee                      string                     `json:"fee"`
	Currency                 string                     `json:"currency"`
	SourceAt                 string                     `json:"source_at"`
	NormalizationID          string                     `json:"normalization_id"`
	LedgerTransactionID      string                     `json:"ledger_transaction_id"`
}

type LocalIssueEvidence struct {
	Reason              ReasonCode     `json:"reason"`
	AccountID           string         `json:"account_id"`
	Provider            venue.Provider `json:"provider"`
	Namespace           string         `json:"namespace"`
	SourceID            string         `json:"source_id"`
	SourceAt            string         `json:"source_at"`
	VenueContractID     string         `json:"venue_contract_id"`
	LedgerTransactionID string         `json:"ledger_transaction_id"`
	EvidenceID          string         `json:"evidence_id"`
}

type localCanonical struct {
	Schema                string               `json:"schema"`
	AccountID             string               `json:"account_id"`
	Provider              venue.Provider       `json:"provider"`
	Namespace             string               `json:"namespace"`
	Currency              string               `json:"currency"`
	HorizonStart          string               `json:"horizon_start"`
	HorizonEnd            string               `json:"horizon_end"`
	CheckpointID          string               `json:"checkpoint_id"`
	CheckpointAsOf        string               `json:"checkpoint_as_of"`
	ProjectionVersion     string               `json:"projection_version"`
	ThroughTransactionID  string               `json:"through_transaction_id"`
	TransactionCount      int                  `json:"transaction_count"`
	InputChecksum         string               `json:"input_checksum"`
	OutputChecksum        string               `json:"output_checksum"`
	AttestationKeyID      string               `json:"attestation_key_id"`
	AttestationHMACSHA256 string               `json:"attestation_hmac_sha256"`
	ProjectionPayload     json.RawMessage      `json:"projection_payload"`
	Cash                  string               `json:"cash"`
	Positions             []LocalPosition      `json:"positions"`
	TransactionIDs        []string             `json:"transaction_ids"`
	Fills                 []LocalFillEvidence  `json:"fills"`
	Issues                []LocalIssueEvidence `json:"issues"`
}

// LocalSnapshot is exact local evidence derived from one verified checkpoint.
type LocalSnapshot struct {
	canonical localCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

// LocalSnapshotRequest is the immutable scope given to the transaction owner.
type LocalSnapshotRequest struct {
	AccountID    uuid.UUID
	Provider     venue.Provider
	Namespace    string
	HorizonStart time.Time
	HorizonEnd   time.Time
	CheckpointID uuid.UUID
}

// LocalEvidenceReader owns one PostgreSQL REPEATABLE READ, read-only
// transaction and must rebuild/verify the projection and load every returned
// lifecycle fact before that same transaction closes.
type LocalEvidenceReader interface {
	ReadLocalEvidenceInRepeatableRead(context.Context, LocalSnapshotRequest) (LocalSnapshotInput, error)
}

type LocalSource struct{ reader LocalEvidenceReader }

func NewLocalSource(reader LocalEvidenceReader) *LocalSource { return &LocalSource{reader: reader} }

func (source *LocalSource) Capture(ctx context.Context, request LocalSnapshotRequest) (*LocalSnapshot, error) {
	if source == nil || source.reader == nil {
		return nil, fmt.Errorf("local reconciliation evidence reader is required")
	}
	input, err := source.reader.ReadLocalEvidenceInRepeatableRead(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("read local reconciliation evidence: %w", err)
	}
	if input.AccountID != request.AccountID || input.Provider != request.Provider || input.Namespace != request.Namespace ||
		!input.HorizonStart.Equal(request.HorizonStart) || !input.HorizonEnd.Equal(request.HorizonEnd) ||
		input.Checkpoint == nil || input.Checkpoint.ID != request.CheckpointID {
		return nil, fmt.Errorf("local reconciliation reader changed requested scope")
	}
	return NewLocalSnapshot(input)
}

// NewLocalSnapshot derives cash and open positions exclusively from canonical
// checkpoint bytes and validates fill membership against the exact transaction set.
func NewLocalSnapshot(input LocalSnapshotInput) (*LocalSnapshot, error) {
	if input.Checkpoint == nil {
		return nil, fmt.Errorf("projection checkpoint is required")
	}
	if err := input.Checkpoint.Validate(); err != nil {
		return nil, fmt.Errorf("validate projection checkpoint: %w", err)
	}
	rule, ok := mustPolicyProvider(input.Provider)
	if !ok || input.Namespace != rule.AuthoritativeFillNamespace || input.AccountID != input.Checkpoint.AccountID ||
		!validEvidenceTime(input.HorizonStart) || !validEvidenceTime(input.HorizonEnd) ||
		!input.HorizonStart.Before(input.HorizonEnd) || !input.HorizonEnd.Equal(input.Checkpoint.AsOf) {
		return nil, fmt.Errorf("local snapshot scope does not match checkpoint")
	}
	cash, positions, err := decodeProjectionEconomics(input.Checkpoint)
	if err != nil {
		return nil, err
	}
	transactionIDs, membership, err := normalizeTransactionMembership(input.TransactionIDs, input.Checkpoint)
	if err != nil {
		return nil, err
	}
	fills, frontierIssues, err := normalizeLocalFills(input, membership)
	if err != nil {
		return nil, err
	}
	issues, err := normalizeLocalIssues(input, append(append([]LocalSnapshotIssue(nil), input.Issues...), frontierIssues...), membership)
	if err != nil {
		return nil, err
	}
	canonical := localCanonical{
		Schema: localSnapshotSchemaV1, AccountID: input.AccountID.String(), Provider: input.Provider,
		Namespace: input.Namespace, Currency: input.Checkpoint.BaseCurrency,
		HorizonStart: canonicalTime(input.HorizonStart), HorizonEnd: canonicalTime(input.HorizonEnd),
		CheckpointID: input.Checkpoint.ID.String(), CheckpointAsOf: canonicalTime(input.Checkpoint.AsOf),
		ProjectionVersion: input.Checkpoint.ProjectionVersion, ThroughTransactionID: input.Checkpoint.ThroughTransactionID.String(),
		TransactionCount: input.Checkpoint.TransactionCount, InputChecksum: input.Checkpoint.InputChecksum,
		OutputChecksum: input.Checkpoint.OutputChecksum, AttestationKeyID: input.Checkpoint.AttestationKeyID,
		AttestationHMACSHA256: hex.EncodeToString(input.Checkpoint.AttestationHMAC),
		ProjectionPayload:     append(json.RawMessage(nil), input.Checkpoint.PayloadBytes...), Cash: cash,
		Positions: positions, TransactionIDs: transactionIDs, Fills: fills, Issues: issues,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal local reconciliation snapshot: %w", err)
	}
	digest := sha256Hex(encoded)
	return &LocalSnapshot{
		canonical: canonical, bytes: encoded, digest: digest,
		id: economicid.DeterministicUUID(localSnapshotDomain, localSnapshotSchemaV1+"@sha256:"+digest),
	}, nil
}

func decodeProjectionEconomics(checkpoint *ledger.ProjectionCheckpoint) (string, []LocalPosition, error) {
	var payload struct {
		Positions []struct {
			InstrumentID string `json:"instrument_id"`
			Open         bool   `json:"open"`
			Quantity     string `json:"quantity"`
		} `json:"positions"`
		Totals struct {
			Cash string `json:"cash"`
		} `json:"totals"`
	}
	if err := json.Unmarshal(checkpoint.PayloadBytes, &payload); err != nil {
		return "", nil, fmt.Errorf("decode projection economics: %w", err)
	}
	cash, err := exactDecimal(payload.Totals.Cash)
	if err != nil {
		return "", nil, fmt.Errorf("projection cash: %w", err)
	}
	positions := make([]LocalPosition, 0, len(payload.Positions))
	seen := make(map[uuid.UUID]struct{}, len(payload.Positions))
	for _, row := range payload.Positions {
		instrumentID, parseErr := uuid.Parse(row.InstrumentID)
		quantity, quantityErr := exactDecimal(row.Quantity)
		if parseErr != nil || instrumentID == uuid.Nil || quantityErr != nil {
			return "", nil, fmt.Errorf("projection position is invalid")
		}
		if _, ok := seen[instrumentID]; ok {
			return "", nil, fmt.Errorf("projection position instrument is duplicated")
		}
		seen[instrumentID] = struct{}{}
		parsed, _ := decimal.NewFromString(quantity)
		if row.Open != !parsed.IsZero() {
			return "", nil, fmt.Errorf("projection position open state contradicts quantity")
		}
		if row.Open {
			positions = append(positions, LocalPosition{InstrumentID: instrumentID, Quantity: quantity})
		}
	}
	sort.Slice(positions, func(i, j int) bool { return positions[i].InstrumentID.String() < positions[j].InstrumentID.String() })
	return cash, positions, nil
}

func normalizeTransactionMembership(ids []uuid.UUID, checkpoint *ledger.ProjectionCheckpoint) ([]string, map[uuid.UUID]struct{}, error) {
	if len(ids) != checkpoint.TransactionCount {
		return nil, nil, fmt.Errorf("checkpoint transaction membership count mismatch")
	}
	membership := make(map[uuid.UUID]struct{}, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			return nil, nil, fmt.Errorf("checkpoint transaction membership contains nil")
		}
		if _, ok := membership[id]; ok {
			return nil, nil, fmt.Errorf("checkpoint transaction membership is duplicated")
		}
		membership[id] = struct{}{}
		result = append(result, id.String())
	}
	if _, ok := membership[checkpoint.ThroughTransactionID]; !ok {
		return nil, nil, fmt.Errorf("checkpoint through transaction is not a member")
	}
	sort.Strings(result)
	return result, membership, nil
}

func normalizeLocalFills(input LocalSnapshotInput, membership map[uuid.UUID]struct{}) ([]LocalFillEvidence, []LocalSnapshotIssue, error) {
	result := make([]LocalFillEvidence, 0, len(input.Fills))
	frontierIssues := make([]LocalSnapshotIssue, 0)
	seen := make(map[string]struct{}, len(input.Fills))
	for _, fill := range input.Fills {
		if fill.FillID == uuid.Nil || fill.IntentID == uuid.Nil || fill.OrderID == uuid.Nil || fill.AccountID != input.AccountID ||
			fill.Provider != input.Provider || fill.Namespace != input.Namespace || fill.InstrumentID == uuid.Nil ||
			fill.VenueContractID == uuid.Nil || fill.NormalizationID == uuid.Nil || fill.LedgerTransactionID == uuid.Nil ||
			!canonicalNonempty(fill.SourceID) || !canonicalNonempty(fill.SourceRevision) ||
			!canonicalNonempty(fill.ExternalOrderID) || !canonicalNonempty(fill.ClientOrderID) ||
			!validEvidenceTime(fill.SourceAt) || fill.SourceAt.Before(input.HorizonStart) || fill.SourceAt.After(input.HorizonEnd) ||
			(fill.Side != lifecycle.SideBuy && fill.Side != lifecycle.SideSell) || !fill.Quantity.IsPositive() || fill.Price.IsNegative() ||
			fill.Fee.IsNegative() || fill.Currency != input.Checkpoint.BaseCurrency {
			return nil, nil, fmt.Errorf("local fill %q is incomplete or outside scope", fill.SourceID)
		}
		if _, ok := membership[fill.LedgerTransactionID]; !ok {
			frontierIssues = append(frontierIssues, LocalSnapshotIssue{
				Reason: ReasonLocalFillAfterFrontier, AccountID: fill.AccountID, Provider: fill.Provider,
				Namespace: fill.Namespace, SourceID: fill.SourceID, SourceAt: fill.SourceAt,
				VenueContractID: fill.VenueContractID, LedgerTransactionID: fill.LedgerTransactionID, EvidenceID: fill.FillID,
			})
			continue
		}
		class := fill.ObservationClass
		if class == "" {
			class = lifecycle.ObservationOrdinary
		}
		key := fill.SourceID
		if class != lifecycle.ObservationOrdinary {
			if class != lifecycle.ObservationCorrection && class != lifecycle.ObservationBust || fill.OriginalFillID == uuid.Nil ||
				!canonicalNonempty(fill.OriginalSourceID) || !canonicalNonempty(fill.ObservationDiscriminator) {
				return nil, nil, fmt.Errorf("local revision evidence is incomplete")
			}
			key = fill.OriginalSourceID + "\x00" + string(class) + "\x00" + fill.ObservationDiscriminator
		}
		if _, ok := seen[key]; ok {
			return nil, nil, fmt.Errorf("local fill comparison identity is duplicated")
		}
		seen[key] = struct{}{}
		result = append(result, LocalFillEvidence{
			FillID: fill.FillID.String(), IntentID: fill.IntentID.String(), OrderID: fill.OrderID.String(),
			SourceID: fill.SourceID, SourceRevision: fill.SourceRevision, ObservationClass: class,
			ObservationDiscriminator: fill.ObservationDiscriminator, OriginalFillID: projectionUUIDString(fill.OriginalFillID),
			OriginalSourceID: fill.OriginalSourceID, ExternalOrderID: fill.ExternalOrderID, ClientOrderID: fill.ClientOrderID,
			InstrumentID: fill.InstrumentID.String(), VenueContractID: fill.VenueContractID.String(), Side: fill.Side,
			Quantity: fill.Quantity.String(), Price: fill.Price.String(), Fee: fill.Fee.String(), Currency: fill.Currency,
			SourceAt: canonicalTime(fill.SourceAt), NormalizationID: fill.NormalizationID.String(),
			LedgerTransactionID: fill.LedgerTransactionID.String(),
		})
	}
	sort.Slice(result, func(i, j int) bool { return localFillSortKey(result[i]) < localFillSortKey(result[j]) })
	return result, frontierIssues, nil
}

func normalizeLocalIssues(input LocalSnapshotInput, values []LocalSnapshotIssue, membership map[uuid.UUID]struct{}) ([]LocalIssueEvidence, error) {
	result := make([]LocalIssueEvidence, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, issue := range values {
		if issue.Reason != ReasonLocalFillIncomplete && issue.Reason != ReasonLocalFillAfterFrontier ||
			issue.AccountID != input.AccountID || issue.Provider != input.Provider || issue.Namespace != input.Namespace ||
			!canonicalNonempty(issue.SourceID) || !validEvidenceTime(issue.SourceAt) ||
			issue.SourceAt.Before(input.HorizonStart) || issue.SourceAt.After(input.HorizonEnd) || issue.VenueContractID == uuid.Nil ||
			issue.LedgerTransactionID == uuid.Nil || issue.EvidenceID == uuid.Nil {
			return nil, fmt.Errorf("local snapshot issue is invalid")
		}
		_, member := membership[issue.LedgerTransactionID]
		if issue.Reason == ReasonLocalFillAfterFrontier && member || issue.Reason == ReasonLocalFillIncomplete && !member {
			return nil, fmt.Errorf("local snapshot issue contradicts checkpoint membership")
		}
		key := string(issue.Reason) + "\x00" + issue.SourceID + "\x00" + issue.LedgerTransactionID.String()
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("local snapshot issue is duplicated")
		}
		seen[key] = struct{}{}
		result = append(result, LocalIssueEvidence{
			Reason: issue.Reason, AccountID: issue.AccountID.String(), Provider: issue.Provider, Namespace: issue.Namespace,
			SourceID: issue.SourceID, SourceAt: canonicalTime(issue.SourceAt), VenueContractID: issue.VenueContractID.String(),
			LedgerTransactionID: issue.LedgerTransactionID.String(), EvidenceID: issue.EvidenceID.String(),
		})
	}
	sort.Slice(result, func(i, j int) bool {
		left := string(result[i].Reason) + "\x00" + result[i].SourceID + "\x00" + result[i].LedgerTransactionID
		right := string(result[j].Reason) + "\x00" + result[j].SourceID + "\x00" + result[j].LedgerTransactionID
		return left < right
	})
	return result, nil
}

func localFillSortKey(value LocalFillEvidence) string {
	if value.ObservationClass == lifecycle.ObservationOrdinary {
		return "0\x00" + value.SourceID
	}
	return "1\x00" + value.OriginalSourceID + "\x00" + string(value.ObservationClass) + "\x00" + value.ObservationDiscriminator
}

func projectionUUIDString(value uuid.UUID) string {
	if value == uuid.Nil {
		return ""
	}
	return value.String()
}

func (snapshot *LocalSnapshot) ID() uuid.UUID {
	if snapshot == nil {
		return uuid.Nil
	}
	return snapshot.id
}

func (snapshot *LocalSnapshot) Digest() string {
	if snapshot == nil {
		return ""
	}
	return snapshot.digest
}

func (snapshot *LocalSnapshot) CanonicalBytes() json.RawMessage {
	if snapshot == nil {
		return nil
	}
	return append(json.RawMessage(nil), snapshot.bytes...)
}

func (snapshot *LocalSnapshot) Cash() decimal.Decimal {
	if snapshot == nil {
		return decimal.Zero
	}
	value, _ := decimal.NewFromString(snapshot.canonical.Cash)
	return value
}

func (snapshot *LocalSnapshot) Positions() []LocalPosition {
	if snapshot == nil {
		return nil
	}
	return append([]LocalPosition(nil), snapshot.canonical.Positions...)
}

func (snapshot *LocalSnapshot) Fills() []LocalFillEvidence {
	if snapshot == nil {
		return nil
	}
	return append([]LocalFillEvidence(nil), snapshot.canonical.Fills...)
}

func (snapshot *LocalSnapshot) Issues() []LocalIssueEvidence {
	if snapshot == nil {
		return nil
	}
	return append([]LocalIssueEvidence(nil), snapshot.canonical.Issues...)
}

func sameLocalSnapshotPayload(left, right *LocalSnapshot) bool {
	return left != nil && right != nil && left.id == right.id && left.digest == right.digest && bytes.Equal(left.bytes, right.bytes)
}
