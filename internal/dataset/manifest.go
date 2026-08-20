package dataset

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const (
	ManifestSchemaV1 = "dataset-manifest-v1"
	manifestDomain   = "dataset-manifest"
	canonicalLayout  = "2006-01-02T15:04:05.000000Z"
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type ObservationInput struct {
	SourceKey     string
	InstrumentID  uuid.UUID
	EffectiveAt   time.Time
	PublishedAt   *time.Time
	ObservedAt    time.Time
	AvailableAt   time.Time
	Revision      string
	CorrectionOf  string
	ContentSHA256 string
	Bid           *string
	Ask           *string
	Volume        *string
	Depth         *string
}

type PartitionInput struct {
	Kind                    Kind
	Provider                string
	Source                  string
	Namespace               string
	RequestSHA256           string
	MediaType               string
	SymbologyVersion        string
	AdjustmentPolicy        string
	Timezone                string
	Calendar                string
	Revision                string
	SupersedesContentSHA256 string
	License                 string
	RetentionPolicy         string
	Observations            []ObservationInput
}

type ManifestInput struct {
	DecisionCutoff time.Time
	Partitions     []PartitionInput
}

type Observation struct {
	Sequence      int     `json:"sequence"`
	SourceKey     string  `json:"source_key"`
	InstrumentID  string  `json:"instrument_id"`
	EffectiveAt   string  `json:"effective_at"`
	PublishedAt   string  `json:"published_at"`
	ObservedAt    string  `json:"observed_at"`
	AvailableAt   string  `json:"available_at"`
	Revision      string  `json:"revision"`
	CorrectionOf  string  `json:"correction_of"`
	ContentSHA256 string  `json:"content_sha256"`
	Bid           *string `json:"bid"`
	Ask           *string `json:"ask"`
	Volume        *string `json:"volume"`
	Depth         *string `json:"depth"`
}

type Partition struct {
	Sequence                int           `json:"sequence"`
	Kind                    Kind          `json:"kind"`
	Provider                string        `json:"provider"`
	Source                  string        `json:"source"`
	Namespace               string        `json:"namespace"`
	RequestSHA256           string        `json:"request_sha256"`
	ContentSHA256           string        `json:"content_sha256"`
	MediaType               string        `json:"media_type"`
	EffectiveStart          string        `json:"effective_start"`
	EffectiveEnd            string        `json:"effective_end"`
	ObservedStart           string        `json:"observed_start"`
	ObservedEnd             string        `json:"observed_end"`
	AvailableStart          string        `json:"available_start"`
	AvailableEnd            string        `json:"available_end"`
	SymbologyVersion        string        `json:"symbology_version"`
	AdjustmentPolicy        string        `json:"adjustment_policy"`
	Timezone                string        `json:"timezone"`
	Calendar                string        `json:"calendar"`
	Revision                string        `json:"revision"`
	SupersedesContentSHA256 string        `json:"supersedes_content_sha256"`
	RowCount                int           `json:"row_count"`
	License                 string        `json:"license"`
	RetentionPolicy         string        `json:"retention_policy"`
	Observations            []Observation `json:"observations"`
}

type manifestCanonical struct {
	Schema           string      `json:"schema"`
	DecisionCutoff   string      `json:"decision_cutoff"`
	PartitionCount   int         `json:"partition_count"`
	ObservationCount int         `json:"observation_count"`
	Partitions       []Partition `json:"partitions"`
}

type Manifest struct {
	canonical manifestCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewManifest(input ManifestInput) (*Manifest, error) {
	if !canonicalTimeValue(input.DecisionCutoff) || len(input.Partitions) == 0 {
		return nil, fmt.Errorf("dataset manifest requires a UTC microsecond cutoff and partitions")
	}
	partitionInputs := clonePartitionInputs(input.Partitions)
	sort.Slice(partitionInputs, func(i, j int) bool {
		return partitionInputKey(partitionInputs[i]) < partitionInputKey(partitionInputs[j])
	})
	partitions := make([]Partition, 0, len(partitionInputs))
	seenPartitions := make(map[string]struct{}, len(partitionInputs))
	observationCount := 0
	for sequence, value := range partitionInputs {
		key := partitionInputKey(value)
		if _, ok := seenPartitions[key]; ok {
			return nil, fmt.Errorf("dataset manifest partition identity is duplicated")
		}
		seenPartitions[key] = struct{}{}
		partition, err := normalizePartition(sequence, value, input.DecisionCutoff)
		if err != nil {
			return nil, err
		}
		observationCount += partition.RowCount
		partitions = append(partitions, partition)
	}
	canonical := manifestCanonical{
		Schema: ManifestSchemaV1, DecisionCutoff: formatTime(input.DecisionCutoff),
		PartitionCount: len(partitions), ObservationCount: observationCount, Partitions: partitions,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal dataset manifest: %w", err)
	}
	digest := hashBytes(encoded)
	return &Manifest{
		canonical: canonical, bytes: encoded, digest: digest,
		id: economicid.DeterministicUUID(manifestDomain, ManifestSchemaV1+"@sha256:"+digest),
	}, nil
}

func normalizePartition(sequence int, input PartitionInput, cutoff time.Time) (Partition, error) {
	if !validKind(input.Kind) || !canonicalRequired(input.Provider) || !canonicalRequired(input.Source) ||
		!canonicalRequired(input.Namespace) || !sha256Pattern.MatchString(input.RequestSHA256) ||
		!canonicalRequired(input.MediaType) || !canonicalRequired(input.SymbologyVersion) ||
		!canonicalRequired(input.AdjustmentPolicy) || !canonicalRequired(input.Timezone) ||
		!canonicalRequired(input.Calendar) || !canonicalToken(input.Revision) ||
		(input.SupersedesContentSHA256 != "" && !sha256Pattern.MatchString(input.SupersedesContentSHA256)) ||
		!canonicalRequired(input.License) || !canonicalRequired(input.RetentionPolicy) || len(input.Observations) == 0 {
		return Partition{}, fmt.Errorf("dataset partition %q metadata is invalid", input.Namespace)
	}
	values := cloneObservationInputs(input.Observations)
	sort.Slice(values, func(i, j int) bool { return observationInputKey(values[i]) < observationInputKey(values[j]) })
	observations := make([]Observation, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	bySourceKey := make(map[string]Observation, len(values))
	for index, value := range values {
		identity := value.SourceKey + "\x00" + value.Revision
		if _, ok := seen[identity]; ok {
			return Partition{}, fmt.Errorf("dataset observation source identity is duplicated")
		}
		seen[identity] = struct{}{}
		observation, err := normalizeObservation(index, value, cutoff)
		if err != nil {
			return Partition{}, err
		}
		if value.CorrectionOf != "" {
			original, ok := bySourceKey[value.CorrectionOf]
			if !ok || original.CorrectionOf != "" || !parseTime(original.AvailableAt).Before(value.AvailableAt) {
				return Partition{}, fmt.Errorf("dataset correction %q does not identify a prior original", value.SourceKey)
			}
		}
		bySourceKey[value.SourceKey] = observation
		observations = append(observations, observation)
	}
	first := observations[0]
	partition := Partition{
		Sequence: sequence, Kind: input.Kind, Provider: input.Provider, Source: input.Source, Namespace: input.Namespace,
		RequestSHA256: input.RequestSHA256, ContentSHA256: aggregateObservationHash(observations), MediaType: input.MediaType,
		EffectiveStart: first.EffectiveAt, EffectiveEnd: first.EffectiveAt,
		ObservedStart: first.ObservedAt, ObservedEnd: first.ObservedAt,
		AvailableStart: first.AvailableAt, AvailableEnd: first.AvailableAt,
		SymbologyVersion: input.SymbologyVersion, AdjustmentPolicy: input.AdjustmentPolicy,
		Timezone: input.Timezone, Calendar: input.Calendar, Revision: input.Revision,
		SupersedesContentSHA256: input.SupersedesContentSHA256, RowCount: len(observations),
		License: input.License, RetentionPolicy: input.RetentionPolicy, Observations: observations,
	}
	for _, value := range observations[1:] {
		partition.EffectiveStart, partition.EffectiveEnd = minMaxTime(partition.EffectiveStart, partition.EffectiveEnd, value.EffectiveAt)
		partition.ObservedStart, partition.ObservedEnd = minMaxTime(partition.ObservedStart, partition.ObservedEnd, value.ObservedAt)
		partition.AvailableStart, partition.AvailableEnd = minMaxTime(partition.AvailableStart, partition.AvailableEnd, value.AvailableAt)
	}
	return partition, nil
}

func normalizeObservation(sequence int, input ObservationInput, cutoff time.Time) (Observation, error) {
	if !canonicalRequired(input.SourceKey) || !canonicalToken(input.Revision) || !canonicalToken(input.CorrectionOf) ||
		!sha256Pattern.MatchString(input.ContentSHA256) || !canonicalTimeValue(input.EffectiveAt) ||
		!canonicalTimeValue(input.ObservedAt) || !canonicalTimeValue(input.AvailableAt) ||
		input.ObservedAt.After(input.AvailableAt) || input.AvailableAt.After(cutoff) {
		return Observation{}, fmt.Errorf("dataset observation %q point-in-time evidence is invalid", input.SourceKey)
	}
	publishedAt := ""
	if input.PublishedAt != nil {
		if !canonicalTimeValue(*input.PublishedAt) || input.PublishedAt.After(input.ObservedAt) {
			return Observation{}, fmt.Errorf("dataset observation %q publication time is invalid", input.SourceKey)
		}
		publishedAt = formatTime(*input.PublishedAt)
	}
	bid, err := normalizeOptionalDecimal(input.Bid, false)
	if err != nil {
		return Observation{}, fmt.Errorf("dataset observation %q bid: %w", input.SourceKey, err)
	}
	ask, err := normalizeOptionalDecimal(input.Ask, false)
	if err != nil {
		return Observation{}, fmt.Errorf("dataset observation %q ask: %w", input.SourceKey, err)
	}
	if (bid == nil) != (ask == nil) {
		return Observation{}, fmt.Errorf("dataset observation %q bid and ask must appear together", input.SourceKey)
	}
	if bid != nil {
		left, _ := decimal.NewFromString(*bid)
		right, _ := decimal.NewFromString(*ask)
		if left.GreaterThan(right) {
			return Observation{}, fmt.Errorf("dataset observation %q has crossed quote", input.SourceKey)
		}
	}
	volume, err := normalizeOptionalDecimal(input.Volume, false)
	if err != nil {
		return Observation{}, fmt.Errorf("dataset observation %q volume: %w", input.SourceKey, err)
	}
	depth, err := normalizeOptionalDecimal(input.Depth, false)
	if err != nil {
		return Observation{}, fmt.Errorf("dataset observation %q depth: %w", input.SourceKey, err)
	}
	instrumentID := ""
	if input.InstrumentID != uuid.Nil {
		instrumentID = input.InstrumentID.String()
	}
	return Observation{
		Sequence: sequence, SourceKey: input.SourceKey, InstrumentID: instrumentID,
		EffectiveAt: formatTime(input.EffectiveAt), PublishedAt: publishedAt,
		ObservedAt: formatTime(input.ObservedAt), AvailableAt: formatTime(input.AvailableAt),
		Revision: input.Revision, CorrectionOf: input.CorrectionOf, ContentSHA256: input.ContentSHA256,
		Bid: bid, Ask: ask, Volume: volume, Depth: depth,
	}, nil
}

func ManifestFromCanonical(id uuid.UUID, digest string, raw []byte) (*Manifest, error) {
	if id == uuid.Nil || !sha256Pattern.MatchString(digest) || hashBytes(raw) != digest {
		return nil, fmt.Errorf("dataset manifest envelope is invalid")
	}
	var canonical manifestCanonical
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&canonical); err != nil {
		return nil, err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	input := ManifestInput{DecisionCutoff: parseTime(canonical.DecisionCutoff)}
	for _, partition := range canonical.Partitions {
		partitionInput := PartitionInput{
			Kind: partition.Kind, Provider: partition.Provider, Source: partition.Source, Namespace: partition.Namespace,
			RequestSHA256: partition.RequestSHA256, MediaType: partition.MediaType,
			SymbologyVersion: partition.SymbologyVersion, AdjustmentPolicy: partition.AdjustmentPolicy,
			Timezone: partition.Timezone, Calendar: partition.Calendar, Revision: partition.Revision,
			SupersedesContentSHA256: partition.SupersedesContentSHA256, License: partition.License,
			RetentionPolicy: partition.RetentionPolicy,
		}
		for _, observation := range partition.Observations {
			observationInput := ObservationInput{
				SourceKey: observation.SourceKey, EffectiveAt: parseTime(observation.EffectiveAt),
				ObservedAt: parseTime(observation.ObservedAt), AvailableAt: parseTime(observation.AvailableAt),
				Revision: observation.Revision, CorrectionOf: observation.CorrectionOf,
				ContentSHA256: observation.ContentSHA256, Bid: cloneString(observation.Bid), Ask: cloneString(observation.Ask),
				Volume: cloneString(observation.Volume), Depth: cloneString(observation.Depth),
			}
			if observation.InstrumentID != "" {
				parsed, err := uuid.Parse(observation.InstrumentID)
				if err != nil {
					return nil, err
				}
				observationInput.InstrumentID = parsed
			}
			if observation.PublishedAt != "" {
				value := parseTime(observation.PublishedAt)
				observationInput.PublishedAt = &value
			}
			partitionInput.Observations = append(partitionInput.Observations, observationInput)
		}
		input.Partitions = append(input.Partitions, partitionInput)
	}
	manifest, err := NewManifest(input)
	if err != nil {
		return nil, err
	}
	if manifest.id != id || manifest.digest != digest || !bytes.Equal(manifest.bytes, raw) ||
		canonical.PartitionCount != manifest.canonical.PartitionCount || canonical.ObservationCount != manifest.canonical.ObservationCount {
		return nil, fmt.Errorf("dataset manifest canonical graph does not reconstruct")
	}
	return manifest, nil
}

func (manifest *Manifest) ID() uuid.UUID {
	if manifest == nil {
		return uuid.Nil
	}
	return manifest.id
}

func (manifest *Manifest) Digest() string {
	if manifest == nil {
		return ""
	}
	return manifest.digest
}

func (manifest *Manifest) CanonicalBytes() json.RawMessage {
	if manifest == nil {
		return nil
	}
	return append(json.RawMessage(nil), manifest.bytes...)
}

func (manifest *Manifest) DecisionCutoff() time.Time {
	if manifest == nil {
		return time.Time{}
	}
	return parseTime(manifest.canonical.DecisionCutoff)
}

func (manifest *Manifest) Partitions() []Partition {
	if manifest == nil {
		return nil
	}
	result := make([]Partition, len(manifest.canonical.Partitions))
	for index, value := range manifest.canonical.Partitions {
		result[index] = clonePartition(value)
	}
	return result
}

func aggregateObservationHash(values []Observation) string {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte("dataset-partition-observations-v1\x00"))
	var length [8]byte
	for _, value := range values {
		encoded, _ := json.Marshal(value)
		binary.BigEndian.PutUint64(length[:], uint64(len(encoded)))
		_, _ = hasher.Write(length[:])
		_, _ = hasher.Write(encoded)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func partitionInputKey(value PartitionInput) string {
	return string(value.Kind) + "\x00" + value.Provider + "\x00" + value.Namespace + "\x00" + value.RequestSHA256 + "\x00" + value.Revision
}

func observationInputKey(value ObservationInput) string {
	return formatTime(value.AvailableAt) + "\x00" + formatTime(value.EffectiveAt) + "\x00" + value.SourceKey + "\x00" + value.Revision
}

func validKind(value Kind) bool {
	index := sort.Search(len(reviewedKinds()), func(index int) bool { return reviewedKinds()[index] >= value })
	kinds := reviewedKinds()
	return index < len(kinds) && kinds[index] == value
}

func canonicalRequired(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= 512
}

func canonicalToken(value string) bool  { return value == strings.TrimSpace(value) && len(value) <= 512 }
func formatTime(value time.Time) string { return value.Format(canonicalLayout) }
func parseTime(value string) time.Time {
	parsed, _ := time.Parse(canonicalLayout, value)
	return parsed
}

func minMaxTime(start, end, value string) (string, string) {
	if value < start {
		start = value
	}
	if value > end {
		end = value
	}
	return start, end
}

func normalizeOptionalDecimal(value *string, allowNegative bool) (*string, error) {
	if value == nil {
		return nil, nil
	}
	if *value == "" || *value != strings.TrimSpace(*value) || strings.ContainsAny(*value, "eE+") {
		return nil, fmt.Errorf("decimal is not canonical")
	}
	parsed, err := decimal.NewFromString(*value)
	if err != nil || !allowNegative && parsed.IsNegative() || parsed.String() != *value {
		return nil, fmt.Errorf("decimal is invalid")
	}
	canonical := parsed.String()
	return &canonical, nil
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneObservationInputs(values []ObservationInput) []ObservationInput {
	result := append([]ObservationInput(nil), values...)
	for index := range result {
		if result[index].PublishedAt != nil {
			value := *result[index].PublishedAt
			result[index].PublishedAt = &value
		}
		result[index].Bid, result[index].Ask = cloneString(result[index].Bid), cloneString(result[index].Ask)
		result[index].Volume, result[index].Depth = cloneString(result[index].Volume), cloneString(result[index].Depth)
	}
	return result
}

func clonePartitionInputs(values []PartitionInput) []PartitionInput {
	result := append([]PartitionInput(nil), values...)
	for index := range result {
		result[index].Observations = cloneObservationInputs(result[index].Observations)
	}
	return result
}

func clonePartition(value Partition) Partition {
	value.Observations = append([]Observation(nil), value.Observations...)
	for index := range value.Observations {
		value.Observations[index].Bid, value.Observations[index].Ask = cloneString(value.Observations[index].Bid), cloneString(value.Observations[index].Ask)
		value.Observations[index].Volume, value.Observations[index].Depth = cloneString(value.Observations[index].Volume), cloneString(value.Observations[index].Depth)
	}
	return value
}
