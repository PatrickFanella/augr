package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/api"
	"github.com/PatrickFanella/get-rich-quick/internal/automation"
	"github.com/PatrickFanella/get-rich-quick/internal/config"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/eventmarkets"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/paper"
	polymarketexecution "github.com/PatrickFanella/get-rich-quick/internal/execution/polymarket"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/metrics"
	"github.com/PatrickFanella/get-rich-quick/internal/notification"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	pgrepo "github.com/PatrickFanella/get-rich-quick/internal/repository/postgres"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNewAPIServerSchemaBehindFailsFast(t *testing.T) {
	origNewDB := runtimeNewDB
	origCurrentSchemaVersion := runtimeCurrentSchemaVersion
	origNewPaperAccountRepo := runtimeNewPaperAccountRepo
	origAfterSchemaGate := runtimeAfterSchemaGate
	origCloseDB := runtimeCloseDB
	defer func() {
		runtimeNewDB = origNewDB
		runtimeCurrentSchemaVersion = origCurrentSchemaVersion
		runtimeNewPaperAccountRepo = origNewPaperAccountRepo
		runtimeAfterSchemaGate = origAfterSchemaGate
		runtimeCloseDB = origCloseDB
	}()

	var closed atomic.Bool
	var proceeded atomic.Bool
	runtimeNewDB = func(context.Context, string) (*pgrepo.DB, error) {
		return &pgrepo.DB{}, nil
	}
	runtimeCurrentSchemaVersion = func(context.Context, *pgxpool.Pool) (int, error) {
		return pgrepo.RequiredSchemaVersion - 1, nil
	}
	runtimeNewPaperAccountRepo = func(*pgrepo.DB) repository.PaperAccountRepository { return stubPaperAccountRepo{} }
	runtimeAfterSchemaGate = func() { proceeded.Store(true) }
	runtimeCloseDB = func(*pgrepo.DB) { closed.Store(true) }

	_, _, _, err := newAPIServer(context.Background(), config.Config{}, slogDiscardLogger())
	if err == nil {
		t.Fatal("newAPIServer() error = nil, want schema mismatch")
	}
	var mismatchErr *runtimeSchemaVersionError
	if !errors.As(err, &mismatchErr) {
		t.Fatalf("newAPIServer() error = %T, want *runtimeSchemaVersionError", err)
	}
	if mismatchErr.State != "behind" {
		t.Fatalf("mismatchErr.State = %q, want behind", mismatchErr.State)
	}
	if mismatchErr.Current != pgrepo.RequiredSchemaVersion-1 {
		t.Fatalf("mismatchErr.Current = %d, want %d", mismatchErr.Current, pgrepo.RequiredSchemaVersion-1)
	}
	if mismatchErr.Required != pgrepo.RequiredSchemaVersion {
		t.Fatalf("mismatchErr.Required = %d, want %d", mismatchErr.Required, pgrepo.RequiredSchemaVersion)
	}
	for _, want := range []string{
		fmt.Sprintf("current version %d", pgrepo.RequiredSchemaVersion-1),
		fmt.Sprintf("required version %d", pgrepo.RequiredSchemaVersion),
		"run migrations, then restart the process",
		"fresh process restart",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
	if proceeded.Load() {
		t.Fatal("runtime proceeded past schema gate on behind schema")
	}
	if !closed.Load() {
		t.Fatal("runtime did not close db on schema mismatch")
	}
}

func TestNewAPIServerSchemaAheadFailsFast(t *testing.T) {
	origNewDB := runtimeNewDB
	origCurrentSchemaVersion := runtimeCurrentSchemaVersion
	origNewPaperAccountRepo := runtimeNewPaperAccountRepo
	origAfterSchemaGate := runtimeAfterSchemaGate
	origCloseDB := runtimeCloseDB
	defer func() {
		runtimeNewDB = origNewDB
		runtimeCurrentSchemaVersion = origCurrentSchemaVersion
		runtimeNewPaperAccountRepo = origNewPaperAccountRepo
		runtimeAfterSchemaGate = origAfterSchemaGate
		runtimeCloseDB = origCloseDB
	}()

	var closed atomic.Bool
	var proceeded atomic.Bool
	runtimeNewDB = func(context.Context, string) (*pgrepo.DB, error) {
		return &pgrepo.DB{}, nil
	}
	runtimeCurrentSchemaVersion = func(context.Context, *pgxpool.Pool) (int, error) {
		return pgrepo.RequiredSchemaVersion + 1, nil
	}
	runtimeAfterSchemaGate = func() { proceeded.Store(true) }
	runtimeCloseDB = func(*pgrepo.DB) { closed.Store(true) }

	_, _, _, err := newAPIServer(context.Background(), config.Config{}, slogDiscardLogger())
	if err == nil {
		t.Fatal("newAPIServer() error = nil, want schema mismatch")
	}
	var mismatchErr *runtimeSchemaVersionError
	if !errors.As(err, &mismatchErr) {
		t.Fatalf("newAPIServer() error = %T, want *runtimeSchemaVersionError", err)
	}
	if mismatchErr.State != "ahead" {
		t.Fatalf("mismatchErr.State = %q, want ahead", mismatchErr.State)
	}
	if mismatchErr.Current != pgrepo.RequiredSchemaVersion+1 {
		t.Fatalf("mismatchErr.Current = %d, want %d", mismatchErr.Current, pgrepo.RequiredSchemaVersion+1)
	}
	if mismatchErr.Required != pgrepo.RequiredSchemaVersion {
		t.Fatalf("mismatchErr.Required = %d, want %d", mismatchErr.Required, pgrepo.RequiredSchemaVersion)
	}
	for _, want := range []string{
		fmt.Sprintf("current version %d", pgrepo.RequiredSchemaVersion+1),
		fmt.Sprintf("required version %d", pgrepo.RequiredSchemaVersion),
		"run migrations, then restart the process",
		"fresh process restart",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
	if proceeded.Load() {
		t.Fatal("runtime proceeded past schema gate on ahead schema")
	}
	if !closed.Load() {
		t.Fatal("runtime did not close db on schema mismatch")
	}
}

func TestNewAPIServerSchemaMatchSucceeds(t *testing.T) {
	origNewDB := runtimeNewDB
	origCurrentSchemaVersion := runtimeCurrentSchemaVersion
	origNewPaperAccountRepo := runtimeNewPaperAccountRepo
	origAfterSchemaGate := runtimeAfterSchemaGate
	origCloseDB := runtimeCloseDB
	origNewServer := runtimeNewServer
	defer func() {
		runtimeNewDB = origNewDB
		runtimeCurrentSchemaVersion = origCurrentSchemaVersion
		runtimeNewPaperAccountRepo = origNewPaperAccountRepo
		runtimeAfterSchemaGate = origAfterSchemaGate
		runtimeCloseDB = origCloseDB
		runtimeNewServer = origNewServer
	}()

	pool, err := pgxpool.New(context.Background(), "postgres://postgres:postgres@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	var proceeded atomic.Bool
	var closed atomic.Bool
	var serverBuilt atomic.Bool
	var automationWired atomic.Bool
	var milestoneEvidenceWired atomic.Bool
	runtimeNewDB = func(context.Context, string) (*pgrepo.DB, error) {
		return &pgrepo.DB{Pool: pool}, nil
	}
	runtimeCurrentSchemaVersion = func(context.Context, *pgxpool.Pool) (int, error) {
		return pgrepo.RequiredSchemaVersion, nil
	}
	runtimeNewPaperAccountRepo = func(*pgrepo.DB) repository.PaperAccountRepository { return stubPaperAccountRepo{} }
	runtimeAfterSchemaGate = func() { proceeded.Store(true) }
	runtimeCloseDB = func(*pgrepo.DB) { closed.Store(true) }
	runtimeNewServer = func(_ api.ServerConfig, deps api.Deps, _ *slog.Logger) (*api.Server, error) {
		serverBuilt.Store(true)
		automationWired.Store(deps.Automation != nil)
		milestoneEvidenceWired.Store(deps.MilestoneEvidence != nil)
		return &api.Server{}, nil
	}

	server, sched, cleanup, err := newAPIServer(context.Background(), config.Config{}, slogDiscardLogger())
	if err != nil {
		t.Fatalf("newAPIServer() error = %v", err)
	}
	if server == nil {
		t.Fatal("newAPIServer() server = nil, want non-nil")
	}
	if sched != nil {
		t.Fatalf("newAPIServer() scheduler = %v, want nil when scheduler disabled", sched)
	}
	smokeServer, smokeSched, smokeCleanup, err := newAPIServer(
		context.Background(), config.Config{Environment: "smoke"}, slogDiscardLogger(),
	)
	if err != nil {
		t.Fatalf("newAPIServer(smoke) error = %v", err)
	}
	if smokeServer == nil || smokeCleanup == nil {
		t.Fatal("newAPIServer(smoke) did not construct server and cleanup")
	}
	if smokeSched != nil {
		t.Fatalf("newAPIServer(smoke) scheduler = %v, want nil when scheduler disabled", smokeSched)
	}
	if cleanup == nil {
		t.Fatal("newAPIServer() cleanup = nil, want non-nil")
	}
	if !proceeded.Load() {
		t.Fatal("runtime did not proceed past schema gate on matching schema")
	}
	if !serverBuilt.Load() {
		t.Fatal("runtime did not continue to server construction on matching schema")
	}
	if automationWired.Load() {
		t.Fatal("runtime wired automation while scheduler was disabled")
	}
	if !milestoneEvidenceWired.Load() {
		t.Fatal("runtime did not wire read-only milestone evidence inspection")
	}
	if closed.Load() {
		t.Fatal("runtime closed db before cleanup on matching schema")
	}

	cleanup()
	smokeCleanup()
	if !closed.Load() {
		t.Fatal("runtime cleanup did not close db on matching schema")
	}
}

func TestNewAPIServerSchemaDBUnreachableFailsBeforeSchemaGate(t *testing.T) {
	origNewDB := runtimeNewDB
	origCurrentSchemaVersion := runtimeCurrentSchemaVersion
	origNewPaperAccountRepo := runtimeNewPaperAccountRepo
	origAfterSchemaGate := runtimeAfterSchemaGate
	origCloseDB := runtimeCloseDB
	defer func() {
		runtimeNewDB = origNewDB
		runtimeCurrentSchemaVersion = origCurrentSchemaVersion
		runtimeNewPaperAccountRepo = origNewPaperAccountRepo
		runtimeAfterSchemaGate = origAfterSchemaGate
		runtimeCloseDB = origCloseDB
	}()

	startupErr := errors.New("postgres: ping database: dial tcp 127.0.0.1:5432: connect: connection refused")
	var schemaVersionChecked atomic.Bool
	var proceeded atomic.Bool
	var closed atomic.Bool
	runtimeNewDB = func(context.Context, string) (*pgrepo.DB, error) {
		return nil, startupErr
	}
	runtimeCurrentSchemaVersion = func(context.Context, *pgxpool.Pool) (int, error) {
		schemaVersionChecked.Store(true)
		return pgrepo.RequiredSchemaVersion, nil
	}
	runtimeNewPaperAccountRepo = func(*pgrepo.DB) repository.PaperAccountRepository { return stubPaperAccountRepo{} }
	runtimeAfterSchemaGate = func() { proceeded.Store(true) }
	runtimeCloseDB = func(*pgrepo.DB) { closed.Store(true) }

	_, _, _, err := newAPIServer(context.Background(), config.Config{}, slogDiscardLogger())
	if !errors.Is(err, startupErr) {
		t.Fatalf("newAPIServer() error = %v, want %v", err, startupErr)
	}
	if schemaVersionChecked.Load() {
		t.Fatal("runtime checked schema version after DB startup failure")
	}
	if proceeded.Load() {
		t.Fatal("runtime proceeded past schema gate after DB startup failure")
	}
	if closed.Load() {
		t.Fatal("runtime closed db on DB startup failure before a db handle existed")
	}
}

func TestNewAPIServerPolymarketResolutionFailureIsNonFatal(t *testing.T) {
	origNewDB := runtimeNewDB
	origCurrentSchemaVersion := runtimeCurrentSchemaVersion
	origNewPaperAccountRepo := runtimeNewPaperAccountRepo
	origAfterSchemaGate := runtimeAfterSchemaGate
	origCloseDB := runtimeCloseDB
	origNewServer := runtimeNewServer
	origHTTPClient := runtimeHTTPClient
	defer func() {
		runtimeNewDB = origNewDB
		runtimeCurrentSchemaVersion = origCurrentSchemaVersion
		runtimeNewPaperAccountRepo = origNewPaperAccountRepo
		runtimeAfterSchemaGate = origAfterSchemaGate
		runtimeCloseDB = origCloseDB
		runtimeNewServer = origNewServer
		runtimeHTTPClient = origHTTPClient
	}()

	pool, err := pgxpool.New(context.Background(), "postgres://postgres:postgres@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	runtimeNewDB = func(context.Context, string) (*pgrepo.DB, error) { return &pgrepo.DB{Pool: pool}, nil }
	runtimeCurrentSchemaVersion = func(context.Context, *pgxpool.Pool) (int, error) { return pgrepo.RequiredSchemaVersion, nil }
	runtimeNewPaperAccountRepo = func(*pgrepo.DB) repository.PaperAccountRepository { return stubPaperAccountRepo{} }
	runtimeAfterSchemaGate = func() {}
	runtimeCloseDB = func(*pgrepo.DB) {}
	runtimeNewServer = func(_ api.ServerConfig, _ api.Deps, _ *slog.Logger) (*api.Server, error) { return &api.Server{}, nil }
	runtimeHTTPClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`not found`)), Header: make(http.Header)}, nil
	})}

	cfg := config.Config{Environment: "development", Database: config.DatabaseConfig{URL: "postgres://ignored"}}
	t.Setenv("POLYMARKET_WS_ENABLED", "true")
	t.Setenv("POLYMARKET_WS_SLUGS", "slug-a")

	server, _, cleanup, err := newAPIServer(context.Background(), cfg, slogDiscardLogger())
	if err != nil {
		t.Fatalf("newAPIServer() error = %v", err)
	}
	if server == nil {
		t.Fatal("newAPIServer() server = nil")
	}
	if cleanup == nil {
		t.Fatal("newAPIServer() cleanup = nil")
	}
}

func TestNewAPIServerWiresAlpacaReconcileAutomationJob(t *testing.T) {
	origNewDB := runtimeNewDB
	origCurrentSchemaVersion := runtimeCurrentSchemaVersion
	origNewPaperAccountRepo := runtimeNewPaperAccountRepo
	origAfterSchemaGate := runtimeAfterSchemaGate
	origCloseDB := runtimeCloseDB
	origNewServer := runtimeNewServer
	defer func() {
		runtimeNewDB = origNewDB
		runtimeCurrentSchemaVersion = origCurrentSchemaVersion
		runtimeNewPaperAccountRepo = origNewPaperAccountRepo
		runtimeAfterSchemaGate = origAfterSchemaGate
		runtimeCloseDB = origCloseDB
		runtimeNewServer = origNewServer
	}()

	pool, err := pgxpool.New(context.Background(), "postgres://postgres:***@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	var capturedDeps api.Deps
	var cleanupCalled atomic.Bool
	runtimeNewDB = func(context.Context, string) (*pgrepo.DB, error) {
		return &pgrepo.DB{Pool: pool}, nil
	}
	runtimeCurrentSchemaVersion = func(context.Context, *pgxpool.Pool) (int, error) {
		return pgrepo.RequiredSchemaVersion, nil
	}
	runtimeNewPaperAccountRepo = func(*pgrepo.DB) repository.PaperAccountRepository { return stubPaperAccountRepo{} }
	runtimeAfterSchemaGate = func() {}
	runtimeCloseDB = func(*pgrepo.DB) { cleanupCalled.Store(true) }
	runtimeNewServer = func(_ api.ServerConfig, deps api.Deps, _ *slog.Logger) (*api.Server, error) {
		capturedDeps = deps
		return &api.Server{}, nil
	}

	cfg := config.Config{
		Environment: "development",
		Database:    config.DatabaseConfig{URL: "postgres://ignored"},
		Features: config.FeatureFlags{
			EnableScheduler:            true,
			EnableTickerDiscovery:      true,
			EnablePolymarketAutomation: true,
		},
		DataProviders: config.DataProviderConfigs{
			Polygon: config.DataProviderConfig{APIKey: "polygon-key"},
		},
		Brokers: config.BrokerConfigs{
			Alpaca: config.BrokerConfig{APIKey: "alpaca-key", APISecret: "alpaca-secret", PaperMode: true},
		},
		Embedding: config.EmbeddingConfig{Model: "nomic-embed-text", Timeout: time.Second},
		LLM:       config.LLMConfig{Providers: config.LLMProviderConfigs{Ollama: config.OllamaConfig{BaseURL: "http://localhost:11434", APIKey: "test-key"}}},
	}

	_, _, cleanup, err := newAPIServer(context.Background(), cfg, slogDiscardLogger())
	if err != nil {
		t.Fatalf("newAPIServer() error = %v", err)
	}
	if capturedDeps.Automation == nil {
		t.Fatal("newAPIServer() automation = nil, want non-nil")
	}
	status := runtimeSingleAutomationJobStatus(t, capturedDeps.Automation, "alpaca_reconcile")
	if status.Name != "alpaca_reconcile" {
		t.Fatalf("status.Name = %q, want alpaca_reconcile", status.Name)
	}
	if got := status.Schedule; got == "" || got == "Manual only" {
		t.Fatalf("status.Schedule = %q, want scheduled job description", got)
	}

	cleanup()
	if !cleanupCalled.Load() {
		t.Fatal("cleanup did not close db")
	}
}

func TestNewAPIServerWiresPolymarketReconcileAutomationJob(t *testing.T) {
	origNewDB := runtimeNewDB
	origCurrentSchemaVersion := runtimeCurrentSchemaVersion
	origNewPaperAccountRepo := runtimeNewPaperAccountRepo
	origAfterSchemaGate := runtimeAfterSchemaGate
	origCloseDB := runtimeCloseDB
	origNewServer := runtimeNewServer
	defer func() {
		runtimeNewDB = origNewDB
		runtimeCurrentSchemaVersion = origCurrentSchemaVersion
		runtimeNewPaperAccountRepo = origNewPaperAccountRepo
		runtimeAfterSchemaGate = origAfterSchemaGate
		runtimeCloseDB = origCloseDB
		runtimeNewServer = origNewServer
	}()

	pool, err := pgxpool.New(context.Background(), "postgres://postgres:***@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	var capturedDeps api.Deps
	runtimeNewDB = func(context.Context, string) (*pgrepo.DB, error) {
		return &pgrepo.DB{Pool: pool}, nil
	}
	runtimeCurrentSchemaVersion = func(context.Context, *pgxpool.Pool) (int, error) {
		return pgrepo.RequiredSchemaVersion, nil
	}
	runtimeNewPaperAccountRepo = func(*pgrepo.DB) repository.PaperAccountRepository { return stubPaperAccountRepo{} }
	runtimeAfterSchemaGate = func() {}
	runtimeCloseDB = func(*pgrepo.DB) {}
	runtimeNewServer = func(_ api.ServerConfig, deps api.Deps, _ *slog.Logger) (*api.Server, error) {
		capturedDeps = deps
		return &api.Server{}, nil
	}

	cfg := config.Config{
		Environment: "development",
		Database:    config.DatabaseConfig{URL: "postgres://ignored"},
		Features: config.FeatureFlags{
			EnableScheduler:            true,
			EnableTickerDiscovery:      true,
			EnablePolymarketAutomation: true,
		},
		DataProviders: config.DataProviderConfigs{
			Polygon: config.DataProviderConfig{APIKey: "polygon-key"},
		},
		Brokers: config.BrokerConfigs{
			Polymarket: config.PolymarketConfig{Address: "0x0000000000000000000000000000000000000001", KeyID: "pm-key", SecretKey: "pm-secret", Passphrase: "pm-passphrase"},
		},
		Embedding: config.EmbeddingConfig{Model: "nomic-embed-text", Timeout: time.Second},
		LLM:       config.LLMConfig{Providers: config.LLMProviderConfigs{Ollama: config.OllamaConfig{BaseURL: "http://localhost:11434", APIKey: "test-key"}}},
	}

	_, _, cleanup, err := newAPIServer(context.Background(), cfg, slogDiscardLogger())
	if err != nil {
		t.Fatalf("newAPIServer() error = %v", err)
	}
	if capturedDeps.Automation == nil {
		t.Fatal("newAPIServer() automation = nil, want non-nil")
	}
	status := runtimeSingleAutomationJobStatus(t, capturedDeps.Automation, "polymarket_reconcile")
	if status.Name != "polymarket_reconcile" {
		t.Fatalf("status.Name = %q, want polymarket_reconcile", status.Name)
	}
	if got := status.Schedule; got == "" || got == "Manual only" {
		t.Fatalf("status.Schedule = %q, want scheduled job description", got)
	}

	cleanup()
}

func TestNewAPIServerWiresKalshiDiscoveryAutomationJob(t *testing.T) {
	origNewDB := runtimeNewDB
	origCurrentSchemaVersion := runtimeCurrentSchemaVersion
	origNewPaperAccountRepo := runtimeNewPaperAccountRepo
	origAfterSchemaGate := runtimeAfterSchemaGate
	origCloseDB := runtimeCloseDB
	origNewServer := runtimeNewServer
	defer func() {
		runtimeNewDB = origNewDB
		runtimeCurrentSchemaVersion = origCurrentSchemaVersion
		runtimeNewPaperAccountRepo = origNewPaperAccountRepo
		runtimeAfterSchemaGate = origAfterSchemaGate
		runtimeCloseDB = origCloseDB
		runtimeNewServer = origNewServer
	}()

	pool, err := pgxpool.New(context.Background(), "postgres://postgres:***@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	defer pool.Close()

	var capturedDeps api.Deps
	runtimeNewDB = func(context.Context, string) (*pgrepo.DB, error) {
		return &pgrepo.DB{Pool: pool}, nil
	}
	runtimeCurrentSchemaVersion = func(context.Context, *pgxpool.Pool) (int, error) {
		return pgrepo.RequiredSchemaVersion, nil
	}
	runtimeNewPaperAccountRepo = func(*pgrepo.DB) repository.PaperAccountRepository { return stubPaperAccountRepo{} }
	runtimeAfterSchemaGate = func() {}
	runtimeCloseDB = func(*pgrepo.DB) {}
	runtimeNewServer = func(_ api.ServerConfig, deps api.Deps, _ *slog.Logger) (*api.Server, error) {
		capturedDeps = deps
		return &api.Server{}, nil
	}

	cfg := config.Config{
		Environment: "development",
		Database:    config.DatabaseConfig{URL: "postgres://ignored"},
		Features: config.FeatureFlags{
			EnableScheduler:       true,
			EnableTickerDiscovery: false,
		},
		Embedding: config.EmbeddingConfig{Model: "nomic-embed-text", Timeout: time.Second},
		LLM:       config.LLMConfig{Providers: config.LLMProviderConfigs{Ollama: config.OllamaConfig{BaseURL: "http://localhost:11434", APIKey: "test-key"}}},
	}

	_, _, cleanup, err := newAPIServer(context.Background(), cfg, slogDiscardLogger())
	if err != nil {
		t.Fatalf("newAPIServer() error = %v", err)
	}
	if capturedDeps.Automation == nil {
		t.Fatal("newAPIServer() automation = nil, want non-nil")
	}
	status := runtimeSingleAutomationJobStatus(t, capturedDeps.Automation, "kalshi_discovery")
	if status.Name != "kalshi_discovery" {
		t.Fatalf("status.Name = %q, want kalshi_discovery", status.Name)
	}
	if !strings.Contains(status.Schedule, "15 * * * *") {
		t.Fatalf("status.Schedule = %q, want kalshi cron", status.Schedule)
	}

	cleanup()
}

func TestNewRuntimeKalshiClientsShareGovernorAndSeparateLabels(t *testing.T) {
	t.Parallel()

	privateKeyPEMB64 := runtimeTestPrivateKeyPEMB64(t)
	cfg := config.Config{Brokers: config.BrokerConfigs{Kalshi: config.KalshiConfig{APIKeyID: "kid", PrivateKeyPEMB64: privateKeyPEMB64, RequestsPerWindow: 1, Window: time.Second}}}
	dataClient, execClient, gov, err := newRuntimeKalshiClients(cfg, metrics.New(), slogDiscardLogger(), nil)
	if err != nil {
		t.Fatalf("newRuntimeKalshiClients() error = %v", err)
	}
	if dataClient == nil || execClient == nil || gov == nil {
		t.Fatalf("newRuntimeKalshiClients() = (%v, %v, %v), want non-nil clients and governor", dataClient, execClient, gov)
	}
	if dataClient == execClient {
		t.Fatal("expected distinct data and execution clients")
	}
	if dataClient.Governor() != execClient.Governor() {
		t.Fatal("expected shared governor instance")
	}
	if dataClient.ClientType() != "data" || execClient.ClientType() != "execution" {
		t.Fatalf("client types = (%q, %q), want (data, execution)", dataClient.ClientType(), execClient.ClientType())
	}
}

func runtimeTestPrivateKeyPEMB64(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}
	return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(block))
}

func TestNewRuntimeKalshiClientsPublicCatalogWithoutLiveCredentials(t *testing.T) {
	t.Parallel()

	cfg := config.Config{Brokers: config.BrokerConfigs{Kalshi: config.KalshiConfig{APIBaseURL: "https://example.com", RequestsPerWindow: 1, Window: time.Second}}}
	dataClient, execClient, gov, err := newRuntimeKalshiClients(cfg, metrics.New(), slogDiscardLogger(), nil)
	if err != nil {
		t.Fatalf("newRuntimeKalshiClients() error = %v", err)
	}
	if dataClient == nil || gov == nil {
		t.Fatalf("newRuntimeKalshiClients() = (%v, %v, %v), want public data client and governor", dataClient, execClient, gov)
	}
	if execClient != nil {
		t.Fatalf("execClient = %v, want nil without full credentials", execClient)
	}
	if dataClient.ClientType() != "data" {
		t.Fatalf("dataClient.ClientType() = %q, want data", dataClient.ClientType())
	}
}

func TestBootstrapPolymarketStopGuardsFiltersAndPaginates(t *testing.T) {
	t.Parallel()

	secret := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32)))
	client := polymarketexecution.NewClient("kid", secret, slogDiscardLogger())
	guard, err := polymarketexecution.NewStopGuard(polymarketexecution.StopGuardConfig{Broker: polymarketexecution.NewBroker(client)})
	if err != nil {
		t.Fatalf("NewStopGuard() error = %v", err)
	}
	runner := &realStrategyRunner{polymarketStopGuard: guard, logger: slogDiscardLogger()}
	firstPage := make([]domain.Position, 0, polymarketBootstrapPageSize)
	for i := 0; i < polymarketBootstrapPageSize-1; i++ {
		firstPage = append(firstPage, domain.Position{MarketType: domain.MarketTypePolymarket, Ticker: fmt.Sprintf("ignore-%d", i), Quantity: 1})
	}
	firstPage = append(firstPage, domain.Position{ID: uuid.New(), MarketType: domain.MarketTypePolymarket, Ticker: "market-one:YES", Side: domain.PositionSideLong, Quantity: 5, StopLoss: floatPtr(0.4)})
	repo := &bootstrapPolymarketPositionRepoStub{pages: [][]domain.Position{
		firstPage,
		{{ID: uuid.New(), MarketType: domain.MarketTypePolymarket, Ticker: "market-three:NO", Side: domain.PositionSideShort, Quantity: 7, TakeProfit: floatPtr(0.6)}},
	}}

	if err := bootstrapPolymarketStopGuards(context.Background(), runner, repo, slogDiscardLogger()); err != nil {
		t.Fatalf("bootstrapPolymarketStopGuards() error = %v", err)
	}
	if got := repo.calls.Load(); got != 2 {
		t.Fatalf("GetOpen call count = %d, want 2 paged calls", got)
	}
	if got := guard.Active(); got != 2 {
		t.Fatalf("guard.Active() = %d, want 2 registered side-qualified polymarket positions", got)
	}
}

func TestNewNotificationManager_DiscordAlertDispatch(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := config.Config{
		Notifications: config.NotificationConfig{
			Discord: config.DiscordNotificationConfig{
				AlertWebhookURL: server.URL,
			},
			Alerts: config.AlertRulesConfig{
				KillSwitch: config.ImmediateAlertRuleConfig{Channels: []string{notification.ChannelDiscord}},
			},
		},
	}

	manager := newNotificationManager(cfg)
	if manager == nil {
		t.Fatal("newNotificationManager() = nil")
	}

	if err := manager.RecordKillSwitchToggle(context.Background(), true, "manual test", time.Now()); err != nil {
		t.Fatalf("RecordKillSwitchToggle() error = %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("discord requests = %d, want 1", requests.Load())
	}
}

func TestNewNotificationManager_N8NAlertDispatch(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := config.Config{
		Notifications: config.NotificationConfig{
			N8N: config.WebhookNotificationConfig{
				URL: server.URL,
			},
			Alerts: config.AlertRulesConfig{
				KillSwitch: config.ImmediateAlertRuleConfig{Channels: []string{notification.ChannelN8N}},
			},
		},
	}

	manager := newNotificationManager(cfg)
	if manager == nil {
		t.Fatal("newNotificationManager() = nil")
	}

	if err := manager.RecordKillSwitchToggle(context.Background(), true, "manual test", time.Now()); err != nil {
		t.Fatalf("RecordKillSwitchToggle() error = %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("n8n requests = %d, want 1", requests.Load())
	}
}

func TestNewNotificationManager_N8NChannelNoopsWhenUnconfigured(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Notifications: config.NotificationConfig{
			Alerts: config.AlertRulesConfig{
				KillSwitch: config.ImmediateAlertRuleConfig{Channels: []string{notification.ChannelN8N}},
			},
		},
	}

	manager := newNotificationManager(cfg)
	if manager == nil {
		t.Fatal("newNotificationManager() = nil")
	}

	if err := manager.RecordKillSwitchToggle(context.Background(), true, "manual test", time.Now()); err != nil {
		t.Fatalf("RecordKillSwitchToggle() error = %v, want nil", err)
	}
}

func TestNewNotificationManager_SkipsDiscordWhenUnconfigured(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Notifications: config.NotificationConfig{
			Alerts: config.AlertRulesConfig{
				KillSwitch: config.ImmediateAlertRuleConfig{Channels: []string{notification.ChannelDiscord}},
			},
		},
	}

	manager := newNotificationManager(cfg)
	if manager == nil {
		t.Fatal("newNotificationManager() = nil")
	}

	if err := manager.RecordKillSwitchToggle(context.Background(), true, "manual test", time.Now()); err == nil {
		t.Fatal("RecordKillSwitchToggle() error = nil, want missing discord notifier error")
	}
}

type stubDecisionRepo struct {
	decisions []domain.AgentDecision
}

func runtimeSingleAutomationJobStatus(t *testing.T, orch *automation.JobOrchestrator, jobName string) automation.JobStatus {
	t.Helper()
	for _, status := range orch.Status() {
		if status.Name == jobName {
			return status
		}
	}
	t.Fatalf("job status %q not found", jobName)
	return automation.JobStatus{}
}

type captureProvider struct{}

func (captureProvider) Complete(_ context.Context, request llm.CompletionRequest) (*llm.CompletionResponse, error) {
	content := ""
	if len(request.Messages) > 0 {
		content = request.Messages[0].Content
	}

	// These tests exercise prompt wiring through the complete production runner.
	// Return role-appropriate structured output now that judge parse failures are
	// intentionally fatal instead of being converted into implicit HOLDs.
	switch {
	case strings.Contains(content, "custom invest judge prompt"), strings.Contains(content, "key_evidence"):
		content = `{"direction":"hold","conviction":5,"key_evidence":["test evidence"],"acknowledged_risks":["test risk"],"rationale":"test hold"}`
	case strings.Contains(content, "custom risk manager prompt"), strings.Contains(content, "adjusted_position_size"):
		content = `{"action":"HOLD","confidence":5,"adjusted_position_size":0,"adjusted_stop_loss":0,"reasoning":"test hold"}`
	case strings.Contains(content, "custom trader prompt"), strings.Contains(content, "entry_type"):
		content = `{"action":"hold","ticker":"AAPL","confidence":0.5,"rationale":"test hold"}`
	}

	return &llm.CompletionResponse{
		Content: content,
		Model:   request.Model,
	}, nil
}

func (s *stubDecisionRepo) Create(context.Context, *domain.AgentDecision) error { return nil }

func (s *stubDecisionRepo) GetByRun(context.Context, uuid.UUID, repository.AgentDecisionFilter, int, int) ([]domain.AgentDecision, error) {
	return s.decisions, nil
}

func (s *stubDecisionRepo) CountByRun(_ context.Context, _ uuid.UUID, _ repository.AgentDecisionFilter) (int, error) {
	return len(s.decisions), nil
}

type stubPipelineRunRepo struct {
	run          *domain.PipelineRun
	err          error
	createErr    error
	updateErr    error
	created      *domain.PipelineRun
	updates      []repository.PipelineRunStatusUpdate
	getByID      bool
	getCalled    bool
	listCalled   bool
	countCalled  bool
	updateCalled bool
}

func (r *stubPipelineRunRepo) Create(_ context.Context, run *domain.PipelineRun) error {
	if r.createErr != nil {
		return r.createErr
	}
	copyRun := *run
	r.created = &copyRun
	return nil
}

func (r *stubPipelineRunRepo) GetByID(context.Context, uuid.UUID) (*domain.PipelineRun, error) {
	r.getByID = true
	return r.run, r.err
}

func (r *stubPipelineRunRepo) Get(context.Context, uuid.UUID, time.Time) (*domain.PipelineRun, error) {
	r.getCalled = true
	panic("unexpected Get call")
}

func (r *stubPipelineRunRepo) List(context.Context, repository.PipelineRunFilter, int, int) ([]domain.PipelineRun, error) {
	r.listCalled = true
	panic("unexpected List call")
}

func (r *stubPipelineRunRepo) Count(context.Context, repository.PipelineRunFilter) (int, error) {
	r.countCalled = true
	return 0, nil
}

func (r *stubPipelineRunRepo) CountBySignal(context.Context, repository.PipelineRunFilter) (map[domain.PipelineSignal]int, error) {
	return map[domain.PipelineSignal]int{}, nil
}

func (r *stubPipelineRunRepo) CountByStatus(context.Context, repository.PipelineRunFilter) (map[domain.PipelineStatus]int, error) {
	return map[domain.PipelineStatus]int{}, nil
}

func (r *stubPipelineRunRepo) UpdateStatus(_ context.Context, _ uuid.UUID, _ time.Time, update repository.PipelineRunStatusUpdate) error {
	r.updateCalled = true
	r.updates = append(r.updates, update)
	return r.updateErr
}

var _ repository.PipelineRunRepository = (*stubPipelineRunRepo)(nil)

func TestSmokeStrategyRunnerDispatchNotifications_RoutesSignalAndDecisionsToN8NAndDiscord(t *testing.T) {
	t.Parallel()

	var n8nRequests atomic.Int32
	n8nServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n8nRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer n8nServer.Close()

	var signalRequests atomic.Int32
	signalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		signalRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer signalServer.Close()

	var decisionRequests atomic.Int32
	decisionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		decisionRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer decisionServer.Close()

	runner := &smokeStrategyRunner{
		decisionRepo: &stubDecisionRepo{decisions: []domain.AgentDecision{
			{AgentRole: domain.AgentRoleTrader, Phase: domain.PhaseTrading, OutputText: `{"action":"buy"}`, CreatedAt: time.Date(2026, 4, 2, 15, 0, 0, 0, time.UTC)},
			{AgentRole: domain.AgentRoleRiskManager, Phase: domain.PhaseRiskDebate, OutputText: `{"action":"buy","confidence":0.92}`, CreatedAt: time.Date(2026, 4, 2, 15, 1, 0, 0, time.UTC)},
		}},
		notificationManager: newNotificationManager(config.Config{
			Notifications: config.NotificationConfig{
				N8N: config.WebhookNotificationConfig{
					URL: n8nServer.URL,
				},
				Discord: config.DiscordNotificationConfig{
					SignalWebhookURL:   signalServer.URL,
					DecisionWebhookURL: decisionServer.URL,
				},
			},
		}),
	}

	runID := uuid.New()
	strategy := domain.Strategy{ID: uuid.New(), Name: "Momentum", Ticker: "AAPL"}
	state := &agent.PipelineState{
		TradingPlan: agent.TradingPlan{Ticker: "AAPL", Rationale: "Breakout confirmed."},
		FinalSignal: agent.FinalSignal{Signal: domain.PipelineSignalBuy, Confidence: 0.92},
	}
	completedAt := time.Date(2026, 4, 2, 15, 2, 0, 0, time.UTC)

	if err := runner.dispatchNotifications(context.Background(), strategy, &domain.PipelineRun{ID: runID, CompletedAt: &completedAt}, state); err != nil {
		t.Fatalf("dispatchNotifications() error = %v", err)
	}

	if n8nRequests.Load() != 3 {
		t.Fatalf("n8n requests = %d, want 3", n8nRequests.Load())
	}
	if signalRequests.Load() != 1 {
		t.Fatalf("signal requests = %d, want 1", signalRequests.Load())
	}
	if decisionRequests.Load() != 2 {
		t.Fatalf("decision requests = %d, want 2", decisionRequests.Load())
	}
}

func TestSmokeStrategyRunnerFindRunUsesGetByID(t *testing.T) {
	t.Parallel()

	runID := uuid.New()
	expected := &domain.PipelineRun{ID: runID}
	repo := &stubPipelineRunRepo{run: expected}
	runner := &smokeStrategyRunner{runRepo: repo}

	got, err := runner.findRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("findRun() error = %v", err)
	}
	if got != expected {
		t.Fatalf("findRun() = %+v, want %+v", got, expected)
	}
	if !repo.getByID {
		t.Fatal("findRun() did not call GetByID")
	}
	if repo.getCalled || repo.listCalled {
		t.Fatal("findRun() fell back to Get/List scanning")
	}
}

func TestRealStrategyRunnerFindRunUsesGetByID(t *testing.T) {
	t.Parallel()

	runID := uuid.New()
	expected := &domain.PipelineRun{ID: runID}
	repo := &stubPipelineRunRepo{run: expected}
	runner := &realStrategyRunner{runRepo: repo}

	got, err := runner.findRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("findRun() error = %v", err)
	}
	if got != expected {
		t.Fatalf("findRun() = %+v, want %+v", got, expected)
	}
	if !repo.getByID {
		t.Fatal("findRun() did not call GetByID")
	}
	if repo.getCalled || repo.listCalled {
		t.Fatal("findRun() fell back to Get/List scanning")
	}
}

func TestSmokeStrategyRunnerFindRunNotFoundWrapsErrNotFound(t *testing.T) {
	t.Parallel()

	runID := uuid.New()
	runner := &smokeStrategyRunner{runRepo: &stubPipelineRunRepo{err: repository.ErrNotFound}}

	got, err := runner.findRun(context.Background(), runID)
	if got != nil {
		t.Fatalf("findRun() run = %+v, want nil", got)
	}
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("findRun() error = %v, want ErrNotFound", err)
	}
	if err == nil || !strings.Contains(err.Error(), runID.String()) {
		t.Fatalf("findRun() error = %v, want run id in error", err)
	}
}

type stubMarketDataService struct {
	ohlcv        []domain.OHLCV
	fundamentals data.Fundamentals
	news         []data.NewsArticle
	social       []data.SocialSentiment
	errOHLCV     error
	errFund      error
	errNews      error
	errSocial    error
}

func (s *stubMarketDataService) GetOHLCV(context.Context, domain.MarketType, string, data.Timeframe, time.Time, time.Time) ([]domain.OHLCV, error) {
	if s.errOHLCV != nil {
		return nil, s.errOHLCV
	}
	return s.ohlcv, nil
}

func (s *stubMarketDataService) GetFundamentals(context.Context, domain.MarketType, string) (data.Fundamentals, error) {
	if s.errFund != nil {
		return data.Fundamentals{}, s.errFund
	}
	return s.fundamentals, nil
}

func (s *stubMarketDataService) GetNews(context.Context, domain.MarketType, string, time.Time, time.Time) ([]data.NewsArticle, error) {
	if s.errNews != nil {
		return nil, s.errNews
	}
	return s.news, nil
}

func (s *stubMarketDataService) GetSocialSentiment(context.Context, domain.MarketType, string, time.Time, time.Time) ([]data.SocialSentiment, error) {
	if s.errSocial != nil {
		return nil, s.errSocial
	}
	return s.social, nil
}

type stubPositionRepo struct{}

func (stubPositionRepo) Create(context.Context, *domain.Position) error            { return nil }
func (stubPositionRepo) CreateAlpacaOwned(context.Context, *domain.Position) error { return nil }
func (stubPositionRepo) Get(_ context.Context, _ uuid.UUID) (*domain.Position, error) {
	return nil, repository.ErrNotFound
}

func (stubPositionRepo) List(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}
func (stubPositionRepo) Update(context.Context, *domain.Position) error { return nil }
func (stubPositionRepo) Delete(context.Context, uuid.UUID) error        { return nil }
func (stubPositionRepo) GetOpen(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}

func (stubPositionRepo) ListOpenAlpacaOwned(context.Context, int, int) ([]domain.Position, error) {
	return nil, nil
}

func (stubPositionRepo) GetByStrategy(context.Context, uuid.UUID, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}

func (stubPositionRepo) Count(context.Context, repository.PositionFilter) (int, error) {
	return 0, nil
}

func (stubPositionRepo) CountOpen(context.Context, repository.PositionFilter) (int, error) {
	return 0, nil
}

func (stubPositionRepo) CountOpenByMarket(context.Context, repository.PositionFilter) (map[domain.MarketType]int, error) {
	return map[domain.MarketType]int{}, nil
}

func (stubPositionRepo) GrossExposureOpen(context.Context, repository.PositionFilter) (float64, error) {
	return 0, nil
}

type stubPaperAccountRepo struct{}

func (stubPaperAccountRepo) ListPaperTrades(context.Context, int, int) ([]domain.Trade, error) {
	return nil, nil
}

func (stubPaperAccountRepo) GetOpenPaperPositions(context.Context, int, int) ([]domain.Position, error) {
	return nil, nil
}

func (stubPaperAccountRepo) ListOpenPaperOrders(context.Context, int, int) ([]domain.Order, error) {
	return nil, nil
}

func (stubPaperAccountRepo) GetMaxPaperExternalIDSequence(context.Context) (uint64, error) {
	return 0, nil
}

type historyPositionRepo struct {
	stubPositionRepo
	positions []domain.Position
}

func (r historyPositionRepo) GetByStrategy(context.Context, uuid.UUID, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return r.positions, nil
}

type metricPositionRepo struct{ count int }

func (m metricPositionRepo) Create(context.Context, *domain.Position) error { return nil }

func (m metricPositionRepo) CreateAlpacaOwned(context.Context, *domain.Position) error { return nil }

func (m metricPositionRepo) Get(context.Context, uuid.UUID) (*domain.Position, error) {
	return nil, repository.ErrNotFound
}

func (m metricPositionRepo) List(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}
func (m metricPositionRepo) Update(context.Context, *domain.Position) error { return nil }
func (m metricPositionRepo) Delete(context.Context, uuid.UUID) error        { return nil }
func (m metricPositionRepo) GetOpen(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}

func (m metricPositionRepo) ListOpenAlpacaOwned(context.Context, int, int) ([]domain.Position, error) {
	return nil, nil
}

func (m metricPositionRepo) GetByStrategy(context.Context, uuid.UUID, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}

func (m metricPositionRepo) Count(context.Context, repository.PositionFilter) (int, error) {
	return m.count, nil
}

func (m metricPositionRepo) CountOpen(context.Context, repository.PositionFilter) (int, error) {
	return m.count, nil
}

func (m metricPositionRepo) CountOpenByMarket(context.Context, repository.PositionFilter) (map[domain.MarketType]int, error) {
	return map[domain.MarketType]int{}, nil
}

func (m metricPositionRepo) GrossExposureOpen(context.Context, repository.PositionFilter) (float64, error) {
	return 0, nil
}

type bootstrapPolymarketPositionRepoStub struct {
	stubPositionRepo
	pages [][]domain.Position
	calls atomic.Int32
}

func (r *bootstrapPolymarketPositionRepoStub) GetOpen(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
	idx := int(r.calls.Add(1) - 1)
	if idx >= len(r.pages) {
		return nil, nil
	}
	return append([]domain.Position(nil), r.pages[idx]...), nil
}

func (r *bootstrapPolymarketPositionRepoStub) ListOpenAlpacaOwned(context.Context, int, int) ([]domain.Position, error) {
	return nil, nil
}

func (r *bootstrapPolymarketPositionRepoStub) CountOpenByMarket(context.Context, repository.PositionFilter) (map[domain.MarketType]int, error) {
	return map[domain.MarketType]int{}, nil
}

func (r *bootstrapPolymarketPositionRepoStub) GrossExposureOpen(context.Context, repository.PositionFilter) (float64, error) {
	return 0, nil
}

func floatPtr(v float64) *float64 { return &v }

func TestSelectedAnalysisRoles_RejectsNonAnalysisRoles(t *testing.T) {
	t.Parallel()

	_, err := selectedAnalysisRoles([]agent.AgentRole{agent.AgentRoleTrader})
	if err == nil {
		t.Fatal("selectedAnalysisRoles() error = nil, want invalid role error")
	}
}

func TestBuildAnalysisAgents_RespectsAnalystSelection(t *testing.T) {
	t.Parallel()

	resolved := agent.ResolvedConfig{
		LLMConfig: agent.ResolvedLLMConfig{QuickThinkModel: "gpt-5-mini"},
		AnalystSelection: []agent.AgentRole{
			agent.AgentRoleNewsAnalyst,
			agent.AgentRoleMarketAnalyst,
		},
	}

	agents, err := buildAnalysisAgents(nil, "openai", resolved, nil, nil)
	if err != nil {
		t.Fatalf("buildAnalysisAgents() error = %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("len(agents) = %d, want 2", len(agents))
	}
	if got := agents[0].Role(); got != agent.AgentRoleMarketAnalyst {
		t.Fatalf("agents[0].Role() = %s, want %s", got, agent.AgentRoleMarketAnalyst)
	}
	if got := agents[1].Role(); got != agent.AgentRoleNewsAnalyst {
		t.Fatalf("agents[1].Role() = %s, want %s", got, agent.AgentRoleNewsAnalyst)
	}
}

func TestRealStrategyRunnerLoadInitialState_PopulatesSeededInputs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	runner := &realStrategyRunner{
		dataService: &stubMarketDataService{
			ohlcv: []domain.OHLCV{
				{Timestamp: now.Add(-24 * time.Hour), Open: 100, High: 105, Low: 99, Close: 104, Volume: 1000},
				{Timestamp: now, Open: 104, High: 109, Low: 103, Close: 108, Volume: 1200},
			},
			fundamentals: data.Fundamentals{Ticker: "AAPL", MarketCap: 3_000_000_000_000, FetchedAt: now},
			news:         []data.NewsArticle{{Title: "AAPL beats", PublishedAt: now, Sentiment: 0.8}},
			social: []data.SocialSentiment{
				{Ticker: "AAPL", Score: 0.2, MeasuredAt: now.Add(-2 * time.Hour)},
				{Ticker: "AAPL", Score: 0.9, MeasuredAt: now.Add(-1 * time.Hour)},
			},
		},
		logger: slogDiscardLogger(),
	}

	seed, err := runner.loadInitialState(context.Background(), domain.Strategy{Ticker: "AAPL", MarketType: domain.MarketTypeStock}, agent.ResolvedConfig{RequiredAnalystRoles: []agent.AgentRole{}})
	if err != nil {
		t.Fatalf("loadInitialState() error = %v", err)
	}
	if seed.Market == nil || len(seed.Market.Bars) != 2 {
		t.Fatalf("seed.Market = %+v, want two bars", seed.Market)
	}
	if len(seed.Market.Indicators) == 0 {
		t.Fatal("seed.Market.Indicators is empty, want computed indicators")
	}
	if seed.Fundamentals == nil || seed.Fundamentals.Ticker != "AAPL" {
		t.Fatalf("seed.Fundamentals = %+v, want AAPL fundamentals", seed.Fundamentals)
	}
	if len(seed.News) != 1 || seed.News[0].Title != "AAPL beats" {
		t.Fatalf("seed.News = %+v, want seeded news", seed.News)
	}
	if seed.Social == nil || seed.Social.Score != 0.9 {
		t.Fatalf("seed.Social = %+v, want latest social snapshot", seed.Social)
	}
	if seed.Market.Indicators[0].Timestamp != now {
		t.Fatalf("indicator timestamp = %s, want %s", seed.Market.Indicators[0].Timestamp, now)
	}
}

func TestRealStrategyRunnerLoadInitialState_DoesNotEmitDebugProgressAtInfo(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))

	runner := &realStrategyRunner{
		dataService: &stubMarketDataService{
			ohlcv: []domain.OHLCV{
				{Timestamp: time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC), Open: 100, High: 105, Low: 99, Close: 104, Volume: 1000},
				{Timestamp: time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC), Open: 104, High: 109, Low: 103, Close: 108, Volume: 1200},
			},
			fundamentals: data.Fundamentals{Ticker: "AAPL"},
			news:         []data.NewsArticle{{Title: "AAPL beats"}},
			social:       []data.SocialSentiment{{Ticker: "AAPL", Score: 0.9, MeasuredAt: time.Date(2026, 4, 5, 11, 0, 0, 0, time.UTC)}},
		},
		logger: slogDiscardLogger(),
	}

	if _, err := runner.loadInitialState(context.Background(), domain.Strategy{Ticker: "AAPL", MarketType: domain.MarketTypeStock}, agent.ResolvedConfig{RequiredAnalystRoles: []agent.AgentRole{}}); err != nil {
		t.Fatalf("loadInitialState() error = %v", err)
	}
	if got := logs.String(); strings.Contains(got, `msg="DEBUG:`) {
		t.Fatalf("found DEBUG-prefixed info log output: %s", got)
	}
}

func TestRealStrategyRunnerNewBrokerForStrategy_ReusesFallbackPaperBroker(t *testing.T) {
	t.Parallel()

	runner := &realStrategyRunner{logger: slogDiscardLogger()}
	strategy := domain.Strategy{
		ID:         uuid.New(),
		Ticker:     "AAPL",
		MarketType: domain.MarketTypeStock,
		IsPaper:    true,
	}

	first, firstName, err := runner.newBrokerForStrategy(strategy)
	if err != nil {
		t.Fatalf("newBrokerForStrategy(first) error = %v", err)
	}
	second, secondName, err := runner.newBrokerForStrategy(strategy)
	if err != nil {
		t.Fatalf("newBrokerForStrategy(second) error = %v", err)
	}
	if firstName != "paper" || secondName != "paper" {
		t.Fatalf("broker names = (%q, %q), want (paper, paper)", firstName, secondName)
	}

	firstPaper, ok := first.(*paper.PaperBroker)
	if !ok {
		t.Fatalf("first broker type = %T, want *paper.PaperBroker", first)
	}
	secondPaper, ok := second.(*paper.PaperBroker)
	if !ok {
		t.Fatalf("second broker type = %T, want *paper.PaperBroker", second)
	}
	if firstPaper != secondPaper {
		t.Fatal("fallback paper broker was recreated, want shared broker instance")
	}
}

func TestRealStrategyRunnerNewOrderManager_WiresRiskPortfolioSnapshot(t *testing.T) {
	t.Parallel()

	positionRepo := stubPositionRepo{}
	engine := risk.NewRiskEngine(risk.DefaultPositionLimits(), risk.DefaultCircuitBreakerConfig(), positionRepo, slogDiscardLogger())
	runner := &realStrategyRunner{
		positionRepo: positionRepo,
		riskEngine:   engine,
		logger:       slogDiscardLogger(),
	}

	_, err := runner.newOrderManager(
		context.Background(),
		domain.Strategy{ID: uuid.New(), Ticker: "AAPL", MarketType: domain.MarketTypeStock, IsPaper: true},
		agent.ResolvedConfig{RiskConfig: agent.ResolvedRiskConfig{PositionSizePct: 10}},
		nil,
	)
	if err != nil {
		t.Fatalf("newOrderManager() error = %v", err)
	}

	status, err := engine.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}
	if status.PositionLimits.CurrentOpenPositions == nil || *status.PositionLimits.CurrentOpenPositions != 0 {
		t.Fatalf("CurrentOpenPositions = %+v, want pointer to 0", status.PositionLimits.CurrentOpenPositions)
	}
	if status.PositionLimits.CurrentTotalExposurePct == nil || *status.PositionLimits.CurrentTotalExposurePct != 0 {
		t.Fatalf("CurrentTotalExposurePct = %+v, want pointer to 0", status.PositionLimits.CurrentTotalExposurePct)
	}
}

func TestSizingConfigForStrategy_UsesMarketDefaults(t *testing.T) {
	t.Parallel()

	resolved := agent.ResolvedConfig{RiskConfig: agent.ResolvedRiskConfig{PositionSizePct: 8, StopLossMultiplier: 1.75}}

	for _, tc := range []struct {
		name   string
		market domain.MarketType
		want   execution.SizingConfig
	}{
		{name: "stock", market: domain.MarketTypeStock, want: execution.SizingConfig{Method: execution.PositionSizingMethodATR, RiskPct: 0.08, ATRMultiplier: 1.75}},
		{name: "crypto", market: domain.MarketTypeCrypto, want: execution.SizingConfig{Method: execution.PositionSizingMethodATR, RiskPct: 0.08, ATRMultiplier: 1.75}},
		{name: "polymarket", market: domain.MarketTypePolymarket, want: execution.SizingConfig{Method: execution.PositionSizingMethodFixedFractional, FractionPct: 0.02}},
		{name: "kalshi", market: domain.MarketTypeKalshi, want: execution.SizingConfig{Method: execution.PositionSizingMethodFixedFractional, FractionPct: 0.02}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := sizingConfigForStrategy(context.Background(), domain.Strategy{ID: uuid.New(), MarketType: tc.market}, nil, resolved, stubPositionRepo{}, slogDiscardLogger())
			if got != tc.want {
				t.Fatalf("sizingConfigForStrategy() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSizingConfigForStrategy_UsesHalfKellyWhenExplicitlyOptedInAndEligible(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	positions := make([]domain.Position, 0, 100)
	for i := 0; i < 60; i++ {
		closedAt := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
		positions = append(positions, domain.Position{ID: uuid.New(), Ticker: "AAPL", Quantity: 1, AvgEntry: 100, RealizedPnL: 2, OpenedAt: closedAt.Add(-time.Hour), ClosedAt: &closedAt})
	}
	for i := 0; i < 40; i++ {
		closedAt := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
		positions = append(positions, domain.Position{ID: uuid.New(), Ticker: "AAPL", Quantity: 1, AvgEntry: 100, RealizedPnL: -1, OpenedAt: closedAt.Add(-time.Hour), ClosedAt: &closedAt})
	}

	runner := &realStrategyRunner{
		positionRepo: historyPositionRepo{stubPositionRepo: stubPositionRepo{}, positions: positions},
		logger:       slogDiscardLogger(),
	}
	useKelly := true
	strategyConfig := &agent.StrategyConfig{RiskConfig: &agent.StrategyRiskConfig{UseKellySizing: &useKelly}}
	resolved := agent.ResolvedConfig{RiskConfig: agent.ResolvedRiskConfig{PositionSizePct: 8, StopLossMultiplier: 1.75}}

	got := sizingConfigForStrategy(context.Background(), domain.Strategy{ID: strategyID, MarketType: domain.MarketTypeStock}, strategyConfig, resolved, runner.positionRepo, slogDiscardLogger())
	if got.Method != execution.PositionSizingMethodKelly || !got.HalfKelly {
		t.Fatalf("sizingConfigForStrategy() = %+v, want half-Kelly", got)
	}
	if got.WinRate != 0.6 || got.WinLossRatio != 2 {
		t.Fatalf("Kelly stats = %+v, want win rate 0.6 and win/loss ratio 2", got)
	}
}

func TestApplyPolymarketSizingCapOnlyAppliesToPolymarket(t *testing.T) {
	t.Parallel()

	base := execution.SizingConfig{Method: execution.PositionSizingMethodATR, RiskPct: 0.08, ATRMultiplier: 1.75}

	if got := applyPolymarketSizingCap(domain.MarketTypeKalshi, base, 500); got != base {
		t.Fatalf("applyPolymarketSizingCap(kalshi) = %+v, want %+v", got, base)
	}

	got := applyPolymarketSizingCap(domain.MarketTypePolymarket, base, 500)
	if !eventmarkets.IsEventMarket(domain.MarketTypePolymarket) {
		t.Fatal("expected polymarket to be recognized as an event market")
	}
	if got.Method != base.Method || got.RiskPct != base.RiskPct || got.ATRMultiplier != base.ATRMultiplier || got.MaxPositionUSDC != 500 {
		t.Fatalf("applyPolymarketSizingCap(polymarket) = %+v, want max position cap applied without changing other fields", got)
	}
}

func TestRuntimeLiveGateForStrategyParsesAllowlists(t *testing.T) {
	t.Parallel()

	strategyID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	runner := &realStrategyRunner{
		cfg: config.Config{
			Features:                     config.FeatureFlags{EnableLiveTrading: true},
			LiveTradingAllowedStrategies: []string{strategyID.String()},
			LiveTradingAllowedBrokers:    []string{"Alpaca", "Binance"},
		},
	}

	gate, err := runner.liveGateForStrategy(domain.Strategy{ID: strategyID, IsPaper: false})
	if err != nil {
		t.Fatalf("liveGateForStrategy() error = %v", err)
	}
	if !gate.EnableLiveTrading {
		t.Fatal("gate.EnableLiveTrading = false, want true")
	}
	if !gate.AllowedStrategies[strategyID] {
		t.Fatal("strategy ID not allowlisted")
	}
	if !gate.AllowedBrokers["alpaca"] || !gate.AllowedBrokers["binance"] {
		t.Fatalf("allowed brokers = %v, want normalized lowercase labels", gate.AllowedBrokers)
	}
}

func TestRuntimeLiveGateForStrategyRejectsInvalidUUID(t *testing.T) {
	t.Parallel()

	runner := &realStrategyRunner{
		cfg: config.Config{
			Features:                     config.FeatureFlags{EnableLiveTrading: true},
			LiveTradingAllowedStrategies: []string{"not-a-uuid"},
			LiveTradingAllowedBrokers:    []string{"alpaca"},
		},
	}

	_, err := runner.liveGateForStrategy(domain.Strategy{IsPaper: false})
	if err == nil {
		t.Fatal("liveGateForStrategy() error = nil, want UUID parse error")
	}
}

func TestRuntimeLiveGateForStrategySkipsPaperParsing(t *testing.T) {
	t.Parallel()

	runner := &realStrategyRunner{
		cfg: config.Config{
			Features:                     config.FeatureFlags{EnableLiveTrading: true},
			LiveTradingAllowedStrategies: []string{"not-a-uuid"},
			LiveTradingAllowedBrokers:    []string{"alpaca"},
		},
	}

	gate, err := runner.liveGateForStrategy(domain.Strategy{IsPaper: true})
	if err != nil {
		t.Fatalf("liveGateForStrategy() error = %v, want nil for paper strategy", err)
	}
	if gate.EnableLiveTrading || len(gate.AllowedStrategies) != 0 || len(gate.AllowedBrokers) != 0 {
		t.Fatalf("gate = %+v, want zero-value gate for paper strategy", gate)
	}
}

func TestRealStrategyRunnerExecutionMetricsHelpers(t *testing.T) {
	t.Parallel()

	positionRepo := metricPositionRepo{count: 2}
	engine := risk.NewRiskEngine(risk.DefaultPositionLimits(), risk.DefaultCircuitBreakerConfig(), positionRepo, slogDiscardLogger())
	if err := engine.ActivateKillSwitch(context.Background(), "test"); err != nil {
		t.Fatalf("ActivateKillSwitch() error = %v", err)
	}
	if err := engine.TripCircuitBreaker(context.Background(), "trip"); err != nil {
		t.Fatalf("TripCircuitBreaker() error = %v", err)
	}
	m := metrics.New()
	runner := &realStrategyRunner{positionRepo: positionRepo, riskEngine: engine, metrics: m}
	completedAt := time.Date(2026, 4, 11, 12, 30, 0, 0, time.UTC)
	runner.recordPipelineMetrics(domain.PipelineRun{
		Ticker:      "AAPL",
		Signal:      domain.PipelineSignalBuy,
		Status:      domain.PipelineStatusCompleted,
		StartedAt:   completedAt.Add(-2 * time.Minute),
		CompletedAt: &completedAt,
	})
	runner.refreshExecutionMetrics(context.Background())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	m.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{"tradingagent_pipeline_runs_total", "ticker=\"AAPL\"", "tradingagent_pipeline_duration_seconds", "tradingagent_positions_open 2", "tradingagent_circuit_breaker_state 1", "tradingagent_kill_switch_active 1"} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics output missing %q", want)
		}
	}
}

func TestBuildRunnerDefinition_AppliesPromptOverridesBeyondAnalysis(t *testing.T) {
	t.Parallel()

	resolved := agent.ResolvedConfig{
		LLMConfig: agent.ResolvedLLMConfig{
			QuickThinkModel: "gpt-5-mini",
			DeepThinkModel:  "gpt-5",
		},
		PromptOverrides: map[agent.AgentRole]string{
			agent.AgentRoleBullResearcher:      "custom bull prompt",
			agent.AgentRoleBearResearcher:      "custom bear prompt",
			agent.AgentRoleInvestJudge:         "custom invest judge prompt",
			agent.AgentRoleTrader:              "custom trader prompt",
			agent.AgentRoleAggressiveAnalyst:   "custom aggressive prompt",
			agent.AgentRoleConservativeAnalyst: "custom conservative prompt",
			agent.AgentRoleNeutralAnalyst:      "custom neutral prompt",
			agent.AgentRoleRiskManager:         "custom risk manager prompt",
		},
	}

	definition, err := buildRunnerDefinition(captureProvider{}, "openai", resolved, 30*time.Second, nil, slogDiscardLogger())
	if err != nil {
		t.Fatalf("buildRunnerDefinition() error = %v", err)
	}

	assertPromptContains := func(label, got, want string) {
		t.Helper()
		if !strings.Contains(got, want) {
			t.Fatalf("%s prompt = %q, want substring %q", label, got, want)
		}
	}

	bullOut, err := definition.Research.Debaters[0].Debate(context.Background(), agent.DebateInput{Ticker: "AAPL"})
	if err != nil {
		t.Fatalf("bull Debate() error = %v", err)
	}
	assertPromptContains("bull", bullOut.LLMResponse.PromptText, "custom bull prompt")

	bearOut, err := definition.Research.Debaters[1].Debate(context.Background(), agent.DebateInput{Ticker: "AAPL"})
	if err != nil {
		t.Fatalf("bear Debate() error = %v", err)
	}
	assertPromptContains("bear", bearOut.LLMResponse.PromptText, "custom bear prompt")

	judgeOut, err := definition.Research.Judge.JudgeResearch(context.Background(), agent.DebateInput{Ticker: "AAPL"})
	if err != nil {
		t.Fatalf("JudgeResearch() error = %v", err)
	}
	assertPromptContains("invest_judge", judgeOut.LLMResponse.PromptText, "custom invest judge prompt")

	traderOut, err := definition.Trader.Trade(context.Background(), agent.TradingInput{Ticker: "AAPL", InvestmentPlan: `{"direction":"buy"}`})
	if err != nil {
		t.Fatalf("Trader.Trade() error = %v", err)
	}
	assertPromptContains("trader", traderOut.LLMResponse.PromptText, "custom trader prompt")

	aggressiveOut, err := definition.Risk.Debaters[0].Debate(context.Background(), agent.DebateInput{Ticker: "AAPL"})
	if err != nil {
		t.Fatalf("aggressive Debate() error = %v", err)
	}
	assertPromptContains("aggressive", aggressiveOut.LLMResponse.PromptText, "custom aggressive prompt")

	conservativeOut, err := definition.Risk.Debaters[1].Debate(context.Background(), agent.DebateInput{Ticker: "AAPL"})
	if err != nil {
		t.Fatalf("conservative Debate() error = %v", err)
	}
	assertPromptContains("conservative", conservativeOut.LLMResponse.PromptText, "custom conservative prompt")

	neutralOut, err := definition.Risk.Debaters[2].Debate(context.Background(), agent.DebateInput{Ticker: "AAPL"})
	if err != nil {
		t.Fatalf("neutral Debate() error = %v", err)
	}
	assertPromptContains("neutral", neutralOut.LLMResponse.PromptText, "custom neutral prompt")

	riskOut, err := definition.Risk.Judge.JudgeRisk(context.Background(), agent.RiskJudgeInput{Ticker: "AAPL", TradingPlan: agent.TradingPlan{Ticker: "AAPL"}})
	if err != nil {
		t.Fatalf("JudgeRisk() error = %v", err)
	}
	assertPromptContains("risk_manager", riskOut.LLMResponse.PromptText, "custom risk manager prompt")
}

func slogDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
