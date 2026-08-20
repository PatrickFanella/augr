// Package financialscheduler defines the deterministic, fenced occurrence
// boundary for scheduled financial work. It deliberately contains no cron,
// provider, deployment, or broker activation.
package financialscheduler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const (
	OccurrenceSchemaV1 = "financial-job-occurrence-v1"
	EffectSchemaV1     = "financial-job-effect-v1"
)

type TriggerKind string

const (
	TriggerScheduled TriggerKind = "scheduled"
	TriggerManual    TriggerKind = "manual"
)

type MutationClass string

const (
	MutationEvidence       MutationClass = "evidence"
	MutationIntentOrder    MutationClass = "intent_order"
	MutationSettlement     MutationClass = "settlement"
	MutationLedger         MutationClass = "ledger"
	MutationReconciliation MutationClass = "reconciliation"
	MutationAllocation     MutationClass = "allocation"
	MutationProvider       MutationClass = "provider"
)

type EffectKind string

const (
	EffectIntent     EffectKind = "execution_intent"
	EffectOrder      EffectKind = "execution_order"
	EffectSettlement EffectKind = "settlement"
	EffectLedger     EffectKind = "ledger"
	EffectAllocation EffectKind = "allocation"
	EffectProvider   EffectKind = "provider_mutation"
)

var normalizedKey = regexp.MustCompile(`^[a-z0-9][a-z0-9_./:-]{0,191}$`)

type OccurrenceInput struct {
	JobKey           string
	ScheduleRevision string
	Trigger          TriggerKind
	DueAt            time.Time
	ManualRequestID  uuid.UUID
}

type Occurrence struct {
	ID               uuid.UUID
	JobKey           string
	ScheduleRevision string
	Trigger          TriggerKind
	DueAt            time.Time
	ManualRequestID  uuid.UUID
	CanonicalJSON    json.RawMessage
	SHA256           string
}

type occurrenceCanonical struct {
	Schema           string `json:"schema"`
	JobKey           string `json:"job_key"`
	ScheduleRevision string `json:"schedule_revision"`
	Trigger          string `json:"trigger"`
	DueAt            string `json:"due_at"`
	ManualRequestID  string `json:"manual_request_id,omitempty"`
}

func NewOccurrence(input OccurrenceInput) (*Occurrence, error) {
	jobKey := strings.TrimSpace(input.JobKey)
	revision := strings.TrimSpace(input.ScheduleRevision)
	dueAt := input.DueAt.UTC().Truncate(time.Microsecond)
	if !normalizedKey.MatchString(jobKey) || jobKey != input.JobKey {
		return nil, fmt.Errorf("financial scheduler: job key is not normalized")
	}
	if revision == "" || revision != input.ScheduleRevision || len(revision) > 256 {
		return nil, fmt.Errorf("financial scheduler: schedule revision is invalid")
	}
	if dueAt.IsZero() {
		return nil, fmt.Errorf("financial scheduler: due time is required")
	}
	if input.Trigger != TriggerScheduled && input.Trigger != TriggerManual {
		return nil, fmt.Errorf("financial scheduler: trigger kind is invalid")
	}
	if input.Trigger == TriggerScheduled && input.ManualRequestID != uuid.Nil {
		return nil, fmt.Errorf("financial scheduler: scheduled occurrence cannot have a manual request")
	}
	if input.Trigger == TriggerManual && input.ManualRequestID == uuid.Nil {
		return nil, fmt.Errorf("financial scheduler: manual occurrence requires a request ID")
	}
	c := occurrenceCanonical{OccurrenceSchemaV1, jobKey, revision, string(input.Trigger), formatTime(dueAt), ""}
	if input.ManualRequestID != uuid.Nil {
		c.ManualRequestID = input.ManualRequestID.String()
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("financial scheduler: encode occurrence: %w", err)
	}
	digest := digestBytes(raw)
	return &Occurrence{
		ID:     economicid.DeterministicUUID("financial-job-occurrence", OccurrenceSchemaV1+"@sha256:"+digest),
		JobKey: jobKey, ScheduleRevision: revision, Trigger: input.Trigger, DueAt: dueAt,
		ManualRequestID: input.ManualRequestID, CanonicalJSON: raw, SHA256: digest,
	}, nil
}

func (o Occurrence) Validate() error {
	rebuilt, err := NewOccurrence(OccurrenceInput{o.JobKey, o.ScheduleRevision, o.Trigger, o.DueAt, o.ManualRequestID})
	if err != nil {
		return err
	}
	if rebuilt.ID != o.ID || rebuilt.SHA256 != o.SHA256 || !jsonEqual(rebuilt.CanonicalJSON, o.CanonicalJSON) {
		return fmt.Errorf("financial scheduler: occurrence identity or content mismatch")
	}
	return nil
}

type EffectInput struct {
	OccurrenceID  uuid.UUID
	Kind          EffectKind
	BusinessKey   string
	PayloadSHA256 string
}

type Effect struct {
	ID            uuid.UUID
	OccurrenceID  uuid.UUID
	Kind          EffectKind
	BusinessKey   string
	PayloadSHA256 string
	CanonicalJSON json.RawMessage
	SHA256        string
}

type effectCanonical struct {
	Schema        string `json:"schema"`
	OccurrenceID  string `json:"occurrence_id"`
	Kind          string `json:"kind"`
	BusinessKey   string `json:"business_key"`
	PayloadSHA256 string `json:"payload_sha256"`
}

func NewEffect(input EffectInput) (*Effect, error) {
	key := strings.TrimSpace(input.BusinessKey)
	if input.OccurrenceID == uuid.Nil || !validEffectKind(input.Kind) {
		return nil, fmt.Errorf("financial scheduler: effect occurrence and kind are required")
	}
	if !normalizedKey.MatchString(key) || key != input.BusinessKey {
		return nil, fmt.Errorf("financial scheduler: effect business key is not normalized")
	}
	if !validSHA256(input.PayloadSHA256) {
		return nil, fmt.Errorf("financial scheduler: effect payload sha256 is invalid")
	}
	c := effectCanonical{EffectSchemaV1, input.OccurrenceID.String(), string(input.Kind), key, input.PayloadSHA256}
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("financial scheduler: encode effect: %w", err)
	}
	digest := digestBytes(raw)
	return &Effect{economicid.DeterministicUUID("financial-job-effect", input.OccurrenceID.String(), string(input.Kind), key), input.OccurrenceID, input.Kind, key, input.PayloadSHA256, raw, digest}, nil
}

func (e Effect) Validate() error {
	rebuilt, err := NewEffect(EffectInput{e.OccurrenceID, e.Kind, e.BusinessKey, e.PayloadSHA256})
	if err != nil {
		return err
	}
	if rebuilt.ID != e.ID || rebuilt.SHA256 != e.SHA256 || !jsonEqual(rebuilt.CanonicalJSON, e.CanonicalJSON) {
		return fmt.Errorf("financial scheduler: effect identity or content mismatch")
	}
	return nil
}

func (e Effect) IntentIdempotencyKey() string { return "financial_effect:v1:" + e.ID.String() }
func (e Effect) OrderIdempotencyKey() string  { return "financial_effect_order:v1:" + e.ID.String() }
func (e Effect) SettlementIdempotencyKey() string {
	return "financial_effect_settlement:v1:" + e.ID.String()
}

type JobDefinition struct {
	Key       string
	Mutations []MutationClass
}

func NewJobDefinition(key string, mutations ...MutationClass) (JobDefinition, error) {
	if !normalizedKey.MatchString(key) {
		return JobDefinition{}, fmt.Errorf("financial scheduler: job definition key is invalid")
	}
	if len(mutations) == 0 {
		return JobDefinition{}, fmt.Errorf("financial scheduler: job definition requires a mutation class")
	}
	seen := make(map[MutationClass]struct{}, len(mutations))
	normalized := make([]MutationClass, 0, len(mutations))
	for _, mutation := range mutations {
		if !validMutationClass(mutation) {
			return JobDefinition{}, fmt.Errorf("financial scheduler: mutation class %q is invalid", mutation)
		}
		if _, exists := seen[mutation]; exists {
			continue
		}
		seen[mutation] = struct{}{}
		normalized = append(normalized, mutation)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i] < normalized[j] })
	return JobDefinition{Key: key, Mutations: normalized}, nil
}

func validMutationClass(value MutationClass) bool {
	switch value {
	case MutationEvidence, MutationIntentOrder, MutationSettlement, MutationLedger, MutationReconciliation, MutationAllocation, MutationProvider:
		return true
	default:
		return false
	}
}

func validEffectKind(value EffectKind) bool {
	switch value {
	case EffectIntent, EffectOrder, EffectSettlement, EffectLedger, EffectAllocation, EffectProvider:
		return true
	default:
		return false
	}
}

func validSHA256(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func digestBytes(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func formatTime(value time.Time) string {
	return value.UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z")
}
func jsonEqual(left, right []byte) bool {
	return bytes.Equal(left, right)
}
