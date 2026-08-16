package simulation

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
)

const (
	observationEvidenceSchema = "simulation-observation-v1"
	fillEvidenceSchema        = "simulation-fill-v1"
)

type simulationObservationEvidence struct {
	Schema            string `json:"schema"`
	Kind              string `json:"kind"`
	Reason            string `json:"reason"`
	OrderID           string `json:"order_id"`
	SnapshotID        string `json:"snapshot_id"`
	PolicyArtifactID  string `json:"policy_artifact_id"`
	PolicyVersion     string `json:"policy_version"`
	Environment       string `json:"environment"`
	EvidenceClass     string `json:"evidence_class"`
	StorageNamespace  string `json:"storage_namespace"`
	EvaluatedAt       string `json:"evaluated_at"`
	SourceAt          string `json:"source_at"`
	ReceivedAt        string `json:"received_at"`
	AvailableAt       string `json:"available_at"`
	RouteSessionLabel string `json:"route_session_label"`
	RouteSessionClose string `json:"route_session_close"`
}

type simulationFillEvidence struct {
	Schema                  string `json:"schema"`
	OrderID                 string `json:"order_id"`
	SnapshotID              string `json:"snapshot_id"`
	Provider                string `json:"provider"`
	Venue                   string `json:"venue"`
	ExchangeAt              string `json:"exchange_at"`
	ReceivedAt              string `json:"received_at"`
	AvailableAt             string `json:"available_at"`
	EvaluatedAt             string `json:"evaluated_at"`
	ExchangeAgeNanoseconds  string `json:"exchange_age_nanoseconds"`
	ReceiveAgeNanoseconds   int64  `json:"receive_age_nanoseconds"`
	AvailableAgeNanoseconds int64  `json:"available_age_nanoseconds"`
	PolicyArtifactID        string `json:"policy_artifact_id"`
	PolicyVersion           string `json:"policy_version"`
	Environment             string `json:"environment"`
	EvidenceClass           string `json:"evidence_class"`
	StorageNamespace        string `json:"storage_namespace"`
	RouteSessionLabel       string `json:"route_session_label"`
	RouteSessionClose       string `json:"route_session_close"`
	FixedLatencyNanoseconds int64  `json:"fixed_latency_nanoseconds"`
	Side                    string `json:"side"`
	OrderType               string `json:"order_type"`
	TimeInForce             string `json:"time_in_force"`
	DepthSide               string `json:"depth_side"`
	DepthLevel              int    `json:"depth_level"`
	DisplayedSize           string `json:"displayed_size"`
	Participation           string `json:"participation"`
	Capacity                string `json:"capacity"`
	Quantity                string `json:"quantity"`
	Price                   string `json:"price"`
	Multiplier              string `json:"multiplier"`
	FeePerOrder             string `json:"fee_per_order"`
	FeePerUnit              string `json:"fee_per_unit"`
	FeeNotionalBPS          string `json:"fee_notional_bps"`
	FeeScale                int32  `json:"fee_scale"`
	FeeAmount               string `json:"fee_amount"`
	FirstFill               bool   `json:"first_fill"`
}

func simulationSourceNamespace(account domain.Account, kind string) (string, error) {
	namespace := strings.Join([]string{
		"simulation",
		string(account.Environment),
		account.StorageNamespace,
		account.EvidenceClass,
		kind + "-v1",
	}, "/")
	if namespace != strings.TrimSpace(namespace) || len(namespace) > 256 {
		return "", fmt.Errorf("simulation source namespace exceeds the lifecycle limit")
	}
	return namespace, nil
}

func simulationFillSourceEventID(orderID, snapshotID string, side marketdata.DepthSide, level int) string {
	return fmt.Sprintf("orders/%s/snapshots/%s/fills/%s/%d", orderID, snapshotID, side, level)
}

func simulationObservationSourceEventID(orderID, snapshotID, kind, discriminator string) string {
	parts := []string{"orders", orderID}
	if snapshotID != "" {
		parts = append(parts, "snapshots", snapshotID)
	}
	parts = append(parts, kind)
	if discriminator != "" {
		parts = append(parts, discriminator)
	}
	return strings.Join(parts, "/")
}

func marshalSimulationObservationEvidence(
	policy *Policy,
	account domain.Account,
	aggregate *lifecycle.Aggregate,
	snapshot *marketdata.QuoteSnapshot,
	kind, reason string,
	evaluatedAt, sourceAt, receivedAt time.Time,
	session *SessionWindow,
) (json.RawMessage, error) {
	if policy == nil || aggregate == nil || aggregate.Order == nil {
		return nil, fmt.Errorf("simulation observation evidence requires policy and routed order")
	}
	value := simulationObservationEvidence{
		Schema: observationEvidenceSchema, Kind: kind, Reason: reason,
		OrderID: aggregate.Order.ID.String(), PolicyArtifactID: policy.ArtifactID().String(),
		PolicyVersion: policy.Version(), Environment: string(account.Environment),
		EvidenceClass: account.EvidenceClass, StorageNamespace: account.StorageNamespace,
		EvaluatedAt: formatSimulationTime(evaluatedAt), SourceAt: formatSimulationTime(sourceAt),
		ReceivedAt: formatSimulationTime(receivedAt),
	}
	if snapshot != nil {
		value.SnapshotID = snapshot.ID.String()
		value.AvailableAt = formatOptionalSimulationTime(snapshot.AvailableAt)
	}
	if session != nil {
		value.RouteSessionLabel = session.Label
		value.RouteSessionClose = formatSimulationTime(session.CloseAt)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal simulation observation evidence: %w", err)
	}
	return encoded, nil
}

func marshalSimulationFillEvidence(
	policy *Policy,
	asset AssetPolicy,
	account domain.Account,
	aggregate *lifecycle.Aggregate,
	snapshot marketdata.QuoteSnapshot,
	assessment marketdata.QuoteAssessment,
	evaluatedAt time.Time,
	session *SessionWindow,
	level marketdata.DepthLevel,
	capacity, quantity, multiplier decimal.Decimal,
	fee *decimal.Decimal,
	firstFill bool,
) (json.RawMessage, error) {
	if aggregate == nil || aggregate.Order == nil {
		return nil, fmt.Errorf("simulation fill evidence requires routed order")
	}
	feeAmount := "0"
	if fee != nil {
		feeAmount = fee.String()
	}
	exchangeAge := ""
	if assessment.ExchangeAge != nil {
		exchangeAge = strconv.FormatInt(assessment.ExchangeAge.Nanoseconds(), 10)
	}
	value := simulationFillEvidence{
		Schema: fillEvidenceSchema, OrderID: aggregate.Order.ID.String(), SnapshotID: snapshot.ID.String(),
		Provider: snapshot.Provider, Venue: snapshot.Venue,
		ExchangeAt: formatOptionalSimulationTime(snapshot.ExchangeAt), ReceivedAt: formatSimulationTime(snapshot.ReceivedAt),
		AvailableAt: formatOptionalSimulationTime(snapshot.AvailableAt), EvaluatedAt: formatSimulationTime(evaluatedAt),
		ExchangeAgeNanoseconds: exchangeAge, ReceiveAgeNanoseconds: assessment.ReceiveAge.Nanoseconds(),
		AvailableAgeNanoseconds: assessment.AvailabilityAge.Nanoseconds(), PolicyArtifactID: policy.ArtifactID().String(),
		PolicyVersion: policy.Version(), Environment: string(account.Environment), EvidenceClass: account.EvidenceClass,
		StorageNamespace: account.StorageNamespace, FixedLatencyNanoseconds: asset.FixedLatency.Nanoseconds(),
		Side: string(aggregate.Order.Side), OrderType: string(aggregate.Order.OrderType),
		TimeInForce: string(aggregate.Order.TimeInForce), DepthSide: string(level.Side), DepthLevel: level.Level,
		DisplayedSize: level.Size.String(), Participation: asset.MaxDepthParticipation.String(),
		Capacity: capacity.String(), Quantity: quantity.String(), Price: level.Price.String(),
		Multiplier: multiplier.String(), FeePerOrder: asset.Fees.PerOrder.String(),
		FeePerUnit: asset.Fees.PerUnit.String(), FeeNotionalBPS: asset.Fees.NotionalBPS.String(),
		FeeScale: asset.Fees.Scale, FeeAmount: feeAmount, FirstFill: firstFill,
	}
	if session != nil {
		value.RouteSessionLabel = session.Label
		value.RouteSessionClose = formatSimulationTime(session.CloseAt)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal simulation fill evidence: %w", err)
	}
	return encoded, nil
}

func formatSimulationTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Truncate(time.Microsecond).Format(policyTimestampLayout)
}

func formatOptionalSimulationTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatSimulationTime(*value)
}
