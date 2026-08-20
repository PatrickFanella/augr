package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/evidenceprogram"
)

type fakeShadowBackend struct {
	benchmark evidenceprogram.EvidenceRef
	versions  map[uuid.UUID]string
	campaign  *evidenceprogram.ShadowCampaign
	days      []*evidenceprogram.ShadowDay
	closed    bool
}

func (b *fakeShadowBackend) BenchmarkReference(_ context.Context, id uuid.UUID) (evidenceprogram.EvidenceRef, error) {
	if id != b.benchmark.ID {
		return evidenceprogram.EvidenceRef{}, errors.New("benchmark not found")
	}
	return b.benchmark, nil
}

func (b *fakeShadowBackend) StrategyCandidate(_ context.Context, key string, id uuid.UUID) (evidenceprogram.ShadowCandidate, error) {
	digest, ok := b.versions[id]
	if !ok {
		return evidenceprogram.ShadowCandidate{}, errors.New("version not found")
	}
	return evidenceprogram.ShadowCandidate{Key: key, VersionID: id, SHA256: digest}, nil
}

func (b *fakeShadowBackend) RegisterCampaign(_ context.Context, value *evidenceprogram.ShadowCampaign) error {
	b.campaign = value
	return nil
}

func (b *fakeShadowBackend) GetCampaign(_ context.Context, id uuid.UUID) (*evidenceprogram.ShadowCampaign, error) {
	if b.campaign == nil || b.campaign.ID() != id {
		return nil, errors.New("campaign not found")
	}
	return b.campaign, nil
}

func (b *fakeShadowBackend) RegisterDay(_ context.Context, value *evidenceprogram.ShadowDay) error {
	b.days = append(b.days, value)
	return nil
}

func (b *fakeShadowBackend) ListDays(context.Context, *evidenceprogram.ShadowCampaign) ([]*evidenceprogram.ShadowDay, error) {
	return append([]*evidenceprogram.ShadowDay(nil), b.days...), nil
}

func (b *fakeShadowBackend) Close() { b.closed = true }

func newFakeShadowBackend() *fakeShadowBackend {
	return &fakeShadowBackend{
		benchmark: evidenceprogram.EvidenceRef{Kind: "benchmark_opportunity_cost_report", ID: uuid.MustParse("70200000-0000-4000-8000-000000000001"), SHA256: strings.Repeat("a", 64)},
		versions: map[uuid.UUID]string{
			uuid.MustParse("70200000-0000-4000-8000-000000000002"): strings.Repeat("b", 64),
			uuid.MustParse("70200000-0000-4000-8000-000000000003"): strings.Repeat("c", 64),
		},
	}
}

func runWithBackend(t *testing.T, backend *fakeShadowBackend, args []string, input string) artifactOutput {
	t.Helper()
	var stdout bytes.Buffer
	err := run(context.Background(), args, strings.NewReader(input), &stdout, func(context.Context, string) (shadowBackend, error) {
		return backend, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var output artifactOutput
	if err = json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if !backend.closed {
		t.Fatal("backend was not closed")
	}
	backend.closed = false
	return output
}

func TestShadowCommandStartRecordAndAssess(t *testing.T) {
	backend := newFakeShadowBackend()
	start := `{
		"key":"operator-shadow-1",
		"started_at":"2026-08-21T00:00:00Z",
		"benchmark_report_id":"70200000-0000-4000-8000-000000000001",
		"candidates":[
			{"key":"beta","strategy_version_id":"70200000-0000-4000-8000-000000000003"},
			{"key":"alpha","strategy_version_id":"70200000-0000-4000-8000-000000000002"}
		]
	}`
	started := runWithBackend(t, backend, []string{"shadow-start", "--db-url", "postgres://local", "--input", "-"}, start)
	if started.Kind != "shadow_campaign" || started.ID == uuid.Nil || started.SHA256 == "" {
		t.Fatalf("start output=%+v", started)
	}

	day := `{
		"campaign_id":"` + started.ID.String() + `",
		"sequence":0,
		"observed_at":"2026-08-21T00:00:00Z",
		"candidates":[
			{"key":"alpha","critical_defects":0,"executable_samples":4,"simulated_fills":3,"slippage_known":true,"slippage_divergence":"0.001"},
			{"key":"beta","critical_defects":0,"executable_samples":5,"simulated_fills":2,"slippage_known":true,"slippage_divergence":"-0.001"}
		],
		"source":{"kind":"local_daily_observation","id":"70200000-0000-4000-8000-000000000004","sha256":"` + strings.Repeat("d", 64) + `"}
	}`
	recorded := runWithBackend(t, backend, []string{"shadow-record-day", "--db-url", "postgres://local", "--input", "-"}, day)
	if recorded.Kind != "shadow_campaign_day" || len(backend.days) != 1 {
		t.Fatalf("record output=%+v days=%d", recorded, len(backend.days))
	}

	assessed := runWithBackend(t, backend, []string{"shadow-assess", "--db-url", "postgres://local", "--campaign-id", started.ID.String()}, "")
	if assessed.Kind != "shadow_30_day" || assessed.Outcome != string(evidenceprogram.OutcomeHeld) || len(assessed.Blockers) == 0 {
		t.Fatalf("assessment output=%+v", assessed)
	}
}

func TestShadowCommandRejectsUnknownFieldsAndSchemaMismatch(t *testing.T) {
	backend := newFakeShadowBackend()
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"shadow-start", "--db-url", "postgres://local"}, strings.NewReader(`{"unexpected":true}`), &stdout, func(context.Context, string) (shadowBackend, error) {
		return backend, nil
	})
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field error=%v", err)
	}

	err = run(context.Background(), []string{"shadow-assess", "--db-url", "postgres://local", "--campaign-id", uuid.NewString()}, strings.NewReader(""), &stdout, func(context.Context, string) (shadowBackend, error) {
		return nil, errors.New("schema version 101 does not match required version 102")
	})
	if err == nil || !strings.Contains(err.Error(), "schema version 101") {
		t.Fatalf("schema mismatch error=%v", err)
	}
}

func TestShadowCommandRejectsNonCanonicalDay(t *testing.T) {
	backend := newFakeShadowBackend()
	campaign, err := evidenceprogram.NewShadowCampaign(evidenceprogram.ShadowCampaignInput{
		Key: "operator-shadow-1", StartedAt: time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC), Benchmark: backend.benchmark,
		Candidates: []evidenceprogram.ShadowCandidate{
			{Key: "alpha", VersionID: uuid.MustParse("70200000-0000-4000-8000-000000000002"), SHA256: strings.Repeat("b", 64)},
			{Key: "beta", VersionID: uuid.MustParse("70200000-0000-4000-8000-000000000003"), SHA256: strings.Repeat("c", 64)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	backend.campaign = campaign
	input := `{
		"campaign_id":"` + campaign.ID().String() + `","sequence":0,"observed_at":"2026-08-22T00:00:00Z",
		"candidates":[],"source":{"kind":"local_daily_observation","id":"70200000-0000-4000-8000-000000000004","sha256":"` + strings.Repeat("d", 64) + `"}
	}`
	var stdout bytes.Buffer
	err = run(context.Background(), []string{"shadow-record-day", "--db-url", "postgres://local"}, strings.NewReader(input), &stdout, func(context.Context, string) (shadowBackend, error) {
		return backend, nil
	})
	if err == nil || !strings.Contains(err.Error(), "exact campaign date") {
		t.Fatalf("noncanonical day error=%v", err)
	}
}
