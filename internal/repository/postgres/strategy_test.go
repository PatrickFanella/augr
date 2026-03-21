package postgres

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestBuildListQuery_NoFilters(t *testing.T) {
	query, args := buildListQuery(repository.StrategyFilter{}, 10, 0)

	if len(args) != 2 {
		t.Fatalf("expected 2 args (limit, offset), got %d", len(args))
	}

	if args[0] != 10 {
		t.Errorf("expected limit=10, got %v", args[0])
	}

	if args[1] != 0 {
		t.Errorf("expected offset=0, got %v", args[1])
	}

	assertContains(t, query, "FROM strategies")
	assertContains(t, query, "ORDER BY created_at DESC")
	assertContains(t, query, "LIMIT $1 OFFSET $2")
	assertNotContains(t, query, "WHERE")
}

func TestBuildListQuery_AllFilters(t *testing.T) {
	active := true
	paper := false

	filter := repository.StrategyFilter{
		Ticker:     "AAPL",
		MarketType: domain.MarketTypeStock,
		IsActive:   &active,
		IsPaper:    &paper,
	}

	query, args := buildListQuery(filter, 25, 50)

	// 4 filter args + limit + offset = 6
	if len(args) != 6 {
		t.Fatalf("expected 6 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "ticker = $1")
	assertContains(t, query, "market_type = $2")
	assertContains(t, query, "is_active = $3")
	assertContains(t, query, "is_paper = $4")
	assertContains(t, query, "LIMIT $5 OFFSET $6")

	if args[0] != "AAPL" {
		t.Errorf("expected ticker arg AAPL, got %v", args[0])
	}

	if args[1] != domain.MarketTypeStock {
		t.Errorf("expected market_type arg stock, got %v", args[1])
	}

	if args[2] != true {
		t.Errorf("expected is_active arg true, got %v", args[2])
	}

	if args[3] != false {
		t.Errorf("expected is_paper arg false, got %v", args[3])
	}
}

func TestBuildListQuery_PartialFilters(t *testing.T) {
	active := true
	filter := repository.StrategyFilter{
		Ticker:   "BTC",
		IsActive: &active,
	}

	query, args := buildListQuery(filter, 10, 0)

	// 2 filter args + limit + offset = 4
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "ticker = $1")
	assertNotContains(t, query, "market_type =")
	assertContains(t, query, "is_active = $2")
	assertNotContains(t, query, "is_paper =")
	assertContains(t, query, "LIMIT $3 OFFSET $4")
}

func TestMarshalConfig_ValidJSON(t *testing.T) {
	input := json.RawMessage(`{"lookback":20,"threshold":0.5}`)

	got, err := marshalConfig(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(got) != `{"lookback":20,"threshold":0.5}` {
		t.Errorf("expected config pass-through, got %s", got)
	}
}

func TestMarshalConfig_NilDefaults(t *testing.T) {
	got, err := marshalConfig(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(got) != "{}" {
		t.Errorf("expected default {}, got %s", got)
	}
}

func TestMarshalConfig_EmptyDefaults(t *testing.T) {
	got, err := marshalConfig(json.RawMessage{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(got) != "{}" {
		t.Errorf("expected default {}, got %s", got)
	}
}

func TestMarshalConfig_InvalidJSON(t *testing.T) {
	_, err := marshalConfig(json.RawMessage(`{not valid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// assertContains fails if substr is not found in s.
func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("expected query to contain %q, got:\n%s", substr, s)
	}
}

// assertNotContains fails if substr is found in s.
func assertNotContains(t *testing.T, s, substr string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Errorf("expected query NOT to contain %q, got:\n%s", substr, s)
	}
}
