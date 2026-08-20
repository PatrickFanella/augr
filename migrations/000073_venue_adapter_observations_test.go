package migrations_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
)

func TestVenueAdapterMigrationDefinesLockedImmutableRawFirstBoundary(t *testing.T) {
	rawSQL := readMigrationFile(t, "000073_venue_adapter_observations.up.sql")
	if first := firstExecutableMigrationSQL(rawSQL); !strings.HasPrefix(first, "lock table execution_orders in share row exclusive mode;") {
		t.Fatalf("migration 73 first executable SQL = %q", first)
	}
	upSQL := normalizeSQL(t, rawSQL)
	for _, fragment := range []string{
		"migration 73 cannot attach pre-existing venue orders without canonical adapter policy artifacts",
		"create function venue_adapter_policy_v1_canonical_bytes",
		"create table venue_adapter_policy_artifacts",
		"provider text not null check (provider in ('alpaca', 'kalshi'))",
		"canonical_bytes = venue_adapter_policy_v1_canonical_bytes(canonical_json)",
		"economic_deterministic_uuid( 'venue-adapter-policy-artifact', policy_version",
		"create index idx_venue_adapter_policy_artifacts_created",
		"create trigger trg_venue_adapter_policy_artifacts_immutable",
		"create table venue_observations",
		"mapped_outcome text not null check (mapped_outcome in ( 'acknowledge', 'no_change', 'fill_notice', 'fill'",
		"raw_bytes bytea not null",
		"raw_json jsonb not null",
		"raw_sha256 = encode(digest(raw_bytes, 'sha256'), 'hex')",
		"raw_json = convert_from(raw_bytes, 'utf8')::jsonb",
		"economic_deterministic_uuid( 'venue-observation', account_id::text, provider, source_namespace, source_event_id",
		"create index idx_venue_observations_order_received",
		"create index idx_venue_observations_recovery",
		"create trigger trg_venue_observations_immutable",
		"create function validate_venue_order_policy_artifact",
		"create trigger trg_execution_orders_venue_policy",
		"create function validate_venue_observation_semantics",
		"create constraint trigger trg_venue_observations_semantics",
		"deferrable initially deferred",
		"create function validate_venue_cancel_command",
		"venue cancellation command requires the canonical persisted binding",
		"create function validate_venue_lifecycle_observation",
		"create constraint trigger trg_execution_lifecycle_venue_observation",
		"create function validate_venue_execution_fill_observation",
		"create constraint trigger trg_execution_fills_venue_observation",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Errorf("migration 73 is missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"insert into orders", "insert into trades", "insert into positions",
		"grant insert", "grant update", "grant delete", "enable_live_trading",
	} {
		if strings.Contains(upSQL, forbidden) {
			t.Errorf("migration 73 contains forbidden activation or legacy mutation %q", forbidden)
		}
	}
}

func TestVenueAdapterMigrationDefinesCompleteEmptyOnlyRollback(t *testing.T) {
	downSQL := normalizeSQL(t, readMigrationFile(t, "000073_venue_adapter_observations.down.sql"))
	for _, fragment := range []string{
		"lock table execution_orders, venue_adapter_policy_artifacts, venue_observations in access exclusive mode",
		"cannot roll back migration 73 while venue adapter artifacts, observations, or orders exist",
		"drop trigger if exists trg_execution_fills_venue_observation on execution_fills",
		"drop trigger if exists trg_execution_lifecycle_venue_observation on execution_lifecycle_events",
		"drop function if exists validate_venue_cancel_command(execution_lifecycle_events)",
		"drop trigger if exists trg_execution_orders_venue_policy on execution_orders",
		"drop trigger if exists trg_venue_observations_immutable on venue_observations",
		"drop table venue_observations",
		"drop table venue_adapter_policy_artifacts",
		"drop function if exists venue_adapter_policy_v1_canonical_bytes(jsonb)",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Errorf("migration 73 rollback is missing %q", fragment)
		}
	}
	for _, preserved := range []string{"execution_orders", "execution_lifecycle_events", "execution_fills", "simulation_policy_artifacts"} {
		if strings.Contains(downSQL, "drop table "+preserved) {
			t.Errorf("migration 73 rollback must preserve %s", preserved)
		}
	}
}

func TestVenueAdapterMigrationAppliesAndEmptyRollbackPreservesSchema72(t *testing.T) {
	ctx, pool, _ := newSimulationPolicyMigrationPool(t)
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000073_venue_adapter_observations.up.sql")); err != nil {
		t.Fatalf("apply migration 73: %v", err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000073_venue_adapter_observations.down.sql")); err != nil {
		t.Fatalf("empty rollback migration 73: %v", err)
	}

	var venueArtifact, observation, simulationArtifact, orderTable *string
	if err := pool.QueryRow(ctx, `SELECT
		to_regclass(current_schema() || '.venue_adapter_policy_artifacts')::TEXT,
		to_regclass(current_schema() || '.venue_observations')::TEXT,
		to_regclass(current_schema() || '.simulation_policy_artifacts')::TEXT,
		to_regclass(current_schema() || '.execution_orders')::TEXT
	`).Scan(&venueArtifact, &observation, &simulationArtifact, &orderTable); err != nil {
		t.Fatal(err)
	}
	if venueArtifact != nil || observation != nil || simulationArtifact == nil || orderTable == nil {
		t.Fatalf("rollback tables = venue:%v observation:%v simulation:%v orders:%v", venueArtifact, observation, simulationArtifact, orderTable)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000073_venue_adapter_observations.up.sql")); err != nil {
		t.Fatalf("reapply migration 73: %v", err)
	}
}

func TestVenueAdapterMigrationAcceptsOnlyExactReviewedPolicyArtifacts(t *testing.T) {
	ctx, pool, _ := newVenueAdapterMigrationPool(t)
	for _, provider := range []venue.Provider{venue.ProviderAlpaca, venue.ProviderKalshi} {
		artifact := venueMigrationArtifact(t, provider)
		if err := insertVenueMigrationArtifact(ctx, pool, artifact); err != nil {
			t.Fatalf("insert %s artifact: %v", provider, err)
		}
		var databaseID uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT economic_deterministic_uuid(
			'venue-adapter-policy-artifact', $1
		)`, artifact.Version).Scan(&databaseID); err != nil {
			t.Fatal(err)
		}
		if databaseID != artifact.ID {
			t.Fatalf("database %s artifact ID = %s, want %s", provider, databaseID, artifact.ID)
		}
	}

	base := venueMigrationArtifact(t, venue.ProviderAlpaca)
	var decoded map[string]any
	if err := json.Unmarshal(base.CanonicalBytes, &decoded); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"unsupported capability": func(value map[string]any) {
			capabilities := value["capabilities"].([]any)
			capabilities[0].(map[string]any)["asset_class"] = "future"
		},
		"missing mapping": func(value map[string]any) {
			mappings := value["mappings"].([]any)
			value["mappings"] = mappings[1:]
		},
		"fee treatment": func(value map[string]any) { value["fee_treatment"] = "invent_zero" },
	} {
		t.Run(name, func(t *testing.T) {
			copyValue := deepCopyMigrationJSON(t, decoded)
			mutate(copyValue)
			forgedBytes, err := json.Marshal(copyValue)
			if err != nil {
				t.Fatal(err)
			}
			forged := forgedVenueMigrationArtifact(forgedBytes, venue.ProviderAlpaca)
			if err := insertVenueMigrationArtifact(ctx, pool, forged); err == nil ||
				!strings.Contains(err.Error(), "canonical venue adapter policy") {
				t.Fatalf("forged artifact error = %v", err)
			}
		})
	}

	for _, statement := range []string{
		`UPDATE venue_adapter_policy_artifacts SET canonical_json = canonical_json || '{"changed":true}'::JSONB
		 WHERE id = (SELECT id FROM venue_adapter_policy_artifacts ORDER BY id LIMIT 1)`,
		`DELETE FROM venue_adapter_policy_artifacts`,
	} {
		if _, err := pool.Exec(ctx, statement); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("artifact mutation error = %v", err)
		}
	}
}

func TestVenueAdapterMigrationRejectsPreexistingVenueOrder(t *testing.T) {
	ctx, pool, fixture := newSimulationPolicyMigrationPool(t)
	intentID := persistRiskApprovedMigrationLifecycle(t, ctx, pool, fixture, "preexisting-venue-73")
	if err := insertMigrationLifecycleOrder(
		t, ctx, pool, fixture, intentID, "preexisting-venue-73", "venue", "legacy-venue-policy",
	); err != nil {
		t.Fatalf("insert preexisting venue order: %v", err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000073_venue_adapter_observations.up.sql")); err == nil ||
		!strings.Contains(err.Error(), "cannot attach pre-existing venue orders") {
		t.Fatalf("migration precondition error = %v", err)
	}
}

func TestVenueAdapterMigrationAuthorizesOnlyReviewedVenueContractAndCapability(t *testing.T) {
	ctx, pool, base := newVenueAdapterMigrationPool(t)
	kalshiArtifact := venueMigrationArtifact(t, venue.ProviderKalshi)
	if err := insertVenueMigrationArtifact(ctx, pool, kalshiArtifact); err != nil {
		t.Fatal(err)
	}

	valid := seedVenueAdapterFixture(t, ctx, pool, base.AccountID, venue.ProviderKalshi,
		`{"kalshi_v2":{"outcome":"no"}}`)
	validIntent := persistRiskApprovedMigrationLifecycle(t, ctx, pool, valid.Common, "kalshi-valid-route")
	if _, err := insertVenueMigrationOrder(ctx, pool, valid, validIntent, "kalshi-valid-route", kalshiArtifact.Version, "gtc"); err != nil {
		t.Fatalf("insert valid Kalshi venue order: %v", err)
	}

	unsupportedIntent := persistRiskApprovedMigrationLifecycle(t, ctx, pool, valid.Common, "kalshi-day-route")
	if _, err := insertVenueMigrationOrder(ctx, pool, valid, unsupportedIntent, "kalshi-day-route", kalshiArtifact.Version, "day"); err == nil ||
		!strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("unsupported Kalshi DAY route error = %v", err)
	}

	missingPolicyFixture := seedVenueAdapterFixture(t, ctx, pool, base.AccountID, venue.ProviderAlpaca, `{}`)
	missingIntent := persistRiskApprovedMigrationLifecycle(t, ctx, pool, missingPolicyFixture.Common, "alpaca-missing-policy")
	missingPolicy := venueMigrationArtifact(t, venue.ProviderAlpaca)
	if _, err := insertVenueMigrationOrder(
		ctx, pool, missingPolicyFixture, missingIntent, "alpaca-missing-policy", missingPolicy.Version, "day",
	); err == nil || !strings.Contains(err.Error(), "registered same-venue") {
		t.Fatalf("route without registered policy error = %v", err)
	}

	for name, metadata := range map[string]string{
		"missing":         `{}`,
		"uppercase":       `{"kalshi_v2":{"outcome":"NO"}}`,
		"non-string":      `{"kalshi_v2":{"outcome":false}}`,
		"extra nested":    `{"kalshi_v2":{"outcome":"no","side":"no"}}`,
		"extra top-level": `{"kalshi_v2":{"outcome":"no"},"outcome":"no"}`,
		"misspelled":      `{"kalshi_v2":{"outome":"no"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			fixture := seedVenueAdapterFixture(t, ctx, pool, base.AccountID, venue.ProviderKalshi, metadata)
			key := "kalshi-metadata-" + strings.ReplaceAll(name, " ", "-")
			intentID := persistRiskApprovedMigrationLifecycle(t, ctx, pool, fixture.Common, key)
			if _, err := insertVenueMigrationOrder(ctx, pool, fixture, intentID, key, kalshiArtifact.Version, "gtc"); err == nil ||
				!strings.Contains(err.Error(), "exact immutable kalshi_v2 outcome metadata") {
				t.Fatalf("invalid Kalshi metadata route error = %v", err)
			}
		})
	}
}

func TestVenueAdapterMigrationPersistsExactObservationAndRejectsChangedIdentity(t *testing.T) {
	ctx, pool, base := newVenueAdapterMigrationPool(t)
	artifact := venueMigrationArtifact(t, venue.ProviderKalshi)
	if err := insertVenueMigrationArtifact(ctx, pool, artifact); err != nil {
		t.Fatal(err)
	}
	fixture := seedVenueAdapterFixture(t, ctx, pool, base.AccountID, venue.ProviderKalshi,
		`{"kalshi_v2":{"outcome":"no"}}`)
	intentID := persistRiskApprovedMigrationLifecycle(t, ctx, pool, fixture.Common, "observation-route")
	orderID, err := insertVenueMigrationOrder(ctx, pool, fixture, intentID, "observation-route", artifact.Version, "gtc")
	if err != nil {
		t.Fatal(err)
	}
	observation := venueMigrationObservation(t, fixture, intentID, orderID, artifact.Version, "fill-1")
	if err := insertVenueMigrationObservation(ctx, pool, observation); err != nil {
		t.Fatalf("insert valid venue observation: %v", err)
	}

	var raw []byte
	var digest string
	if err := pool.QueryRow(ctx, `SELECT raw_bytes, raw_sha256 FROM venue_observations WHERE id = $1`, observation.ID).Scan(&raw, &digest); err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(observation.RawPayload) || digest != observation.PayloadSHA256 {
		t.Fatalf("stored raw evidence = %q/%q", raw, digest)
	}

	for name, statement := range map[string]string{
		"update": `UPDATE venue_observations SET source_revision = 'changed' WHERE id = '` + observation.ID.String() + `'`,
		"delete": `DELETE FROM venue_observations WHERE id = '` + observation.ID.String() + `'`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, statement); err == nil || !strings.Contains(err.Error(), "append-only") {
				t.Fatalf("observation mutation error = %v", err)
			}
		})
	}

	changed := *observation
	changed.SourceRevision = "revision-2"
	if err := insertVenueMigrationObservation(ctx, pool, &changed); err == nil {
		t.Fatal("changed observation identity unexpectedly inserted")
	}

	if _, err := pool.Exec(ctx, `INSERT INTO venue_observations (
		id, account_id, intent_id, order_id, venue_contract_id, provider, venue,
		policy_version, kind, provider_state, mapped_outcome, client_order_id,
		provider_contract_id, canonical_outcome, provider_book_side, provider_action,
		provider_price, identity_kind, source_namespace, source_event_id,
		source_revision, source_at, received_at, raw_bytes, raw_sha256, raw_json, created_at
	) VALUES (
		economic_deterministic_uuid('venue-observation',$1::TEXT,'kalshi','kalshi/portfolio/fills','duplicate-json'),
		$1,$2,$3,$4,'kalshi','kalshi',$5,'fill','fill','fill',$3::TEXT,$6,'no','ask','buy',0.58,
		'provider','kalshi/portfolio/fills','duplicate-json','1',$7,$8,$9,
		encode(digest($9,'sha256'),'hex'),convert_from($9,'UTF8')::JSONB,$8
	)`, fixture.Common.AccountID, intentID, orderID, fixture.Common.VenueContractID,
		artifact.Version, fixture.ContractID, observation.SourceAt, observation.ReceivedAt,
		[]byte(`{"id":"one","id":"two"}`),
	); err == nil {
		t.Fatal("duplicate-key raw JSON unexpectedly inserted")
	}
}

func TestVenueAdapterMigrationConstrainsKalshiProjectionButPreservesContradiction(t *testing.T) {
	ctx, pool, base := newVenueAdapterMigrationPool(t)
	artifact := venueMigrationArtifact(t, venue.ProviderKalshi)
	if err := insertVenueMigrationArtifact(ctx, pool, artifact); err != nil {
		t.Fatal(err)
	}
	fixture := seedVenueAdapterFixture(t, ctx, pool, base.AccountID, venue.ProviderKalshi,
		`{"kalshi_v2":{"outcome":"no"}}`)
	intentID := persistRiskApprovedMigrationLifecycle(t, ctx, pool, fixture.Common, "projection-route")
	orderID, err := insertVenueMigrationOrder(ctx, pool, fixture, intentID, "projection-route", artifact.Version, "gtc")
	if err != nil {
		t.Fatal(err)
	}

	invalid := venueMigrationObservation(t, fixture, intentID, orderID, artifact.Version, "wrong-projection")
	invalid.CanonicalOutcome = "yes"
	if err := insertVenueMigrationObservation(ctx, pool, invalid); err == nil ||
		!strings.Contains(err.Error(), "contradicts immutable contract") {
		t.Fatalf("valid-labelled wrong projection error = %v", err)
	}

	contradictionInput := venueObservationInput(fixture, intentID, orderID, artifact.Version, "wrong-projection-raw")
	contradictionInput.CanonicalOutcome = "yes"
	contradictionInput.ProviderBookSide = "bid"
	contradictionInput.MappedOutcome = venue.OutcomeContradiction
	contradictionInput.RawPayload = json.RawMessage(`{"fill_id":"wrong-projection-raw","outcome":"YES"}`)
	contradiction, err := venue.NewObservation(contradictionInput)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertVenueMigrationObservation(ctx, pool, contradiction); err != nil {
		t.Fatalf("raw contradiction was not preserved: %v", err)
	}
}

func TestVenueAdapterMigrationRequiresRawObservationBeforeProviderLifecycleEvent(t *testing.T) {
	ctx, pool, base := newVenueAdapterMigrationPool(t)
	artifact := venueMigrationArtifact(t, venue.ProviderKalshi)
	if err := insertVenueMigrationArtifact(ctx, pool, artifact); err != nil {
		t.Fatal(err)
	}

	t.Run("with observation", func(t *testing.T) {
		fixture := seedVenueAdapterFixture(t, ctx, pool, base.AccountID, venue.ProviderKalshi,
			`{"kalshi_v2":{"outcome":"yes"}}`)
		intentID := persistRiskApprovedMigrationLifecycle(t, ctx, pool, fixture.Common, "ack-with-raw")
		orderID, err := insertVenueMigrationOrder(ctx, pool, fixture, intentID, "ack-with-raw", artifact.Version, "gtc")
		if err != nil {
			t.Fatal(err)
		}
		observation := venueMigrationAcknowledgement(t, fixture, intentID, orderID, artifact.Version, "ack-1")
		if err := insertVenueMigrationObservation(ctx, pool, observation); err != nil {
			t.Fatal(err)
		}
		if err := insertVenueMigrationAcknowledgement(ctx, pool, fixture, intentID, orderID, artifact.Version, observation); err != nil {
			t.Fatalf("provider acknowledgement with prior raw observation: %v", err)
		}
	})

	t.Run("without observation", func(t *testing.T) {
		fixture := seedVenueAdapterFixture(t, ctx, pool, base.AccountID, venue.ProviderKalshi,
			`{"kalshi_v2":{"outcome":"yes"}}`)
		intentID := persistRiskApprovedMigrationLifecycle(t, ctx, pool, fixture.Common, "ack-without-raw")
		orderID, err := insertVenueMigrationOrder(ctx, pool, fixture, intentID, "ack-without-raw", artifact.Version, "gtc")
		if err != nil {
			t.Fatal(err)
		}
		observation := venueMigrationAcknowledgement(t, fixture, intentID, orderID, artifact.Version, "missing-ack")
		if err := insertVenueMigrationAcknowledgement(ctx, pool, fixture, intentID, orderID, artifact.Version, observation); err == nil ||
			!strings.Contains(err.Error(), "exact prior raw observation") {
			t.Fatalf("provider event without raw observation error = %v", err)
		}
	})
}

func TestVenueAdapterMigrationNonemptyRollbackRefusesAndPreservesFacts(t *testing.T) {
	ctx, pool, _ := newVenueAdapterMigrationPool(t)
	artifact := venueMigrationArtifact(t, venue.ProviderAlpaca)
	if err := insertVenueMigrationArtifact(ctx, pool, artifact); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000073_venue_adapter_observations.down.sql")); err == nil ||
		!strings.Contains(err.Error(), "cannot roll back migration 73") {
		t.Fatalf("nonempty rollback error = %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM venue_adapter_policy_artifacts`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("rollback refusal preserved artifact count = %d, want 1", count)
	}
}

func firstExecutableMigrationSQL(raw string) string {
	var executable []string
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		executable = append(executable, strings.ToLower(trimmed))
	}
	return strings.Join(executable, " ")
}

type venueAdapterMigrationFixture struct {
	Common     commonExecutionLifecycleMigrationFixture
	Provider   venue.Provider
	ContractID string
	Outcome    string
	LimitPrice string
}

func seedVenueAdapterFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	accountID uuid.UUID,
	provider venue.Provider,
	metadata string,
) venueAdapterMigrationFixture {
	t.Helper()
	decisionAt := venueMigrationTime().Add(time.Hour)
	fixture := venueAdapterMigrationFixture{
		Common: commonExecutionLifecycleMigrationFixture{
			AccountID: accountID, InstrumentID: uuid.New(), VenueContractID: uuid.New(),
			QuoteSnapshotID: uuid.New(), DecisionAt: decisionAt,
		},
		Provider: provider, ContractID: strings.ToUpper(string(provider)) + "-" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")),
		LimitPrice: "10.25",
	}
	assetClass := "equity"
	settlement := "physical"
	if provider == venue.ProviderKalshi {
		assetClass = "prediction_contract"
		settlement = "binary"
		fixture.LimitPrice = "0.42"
		var decoded struct {
			Kalshi struct {
				Outcome string `json:"outcome"`
			} `json:"kalshi_v2"`
		}
		_ = json.Unmarshal([]byte(metadata), &decoded)
		fixture.Outcome = decoded.Kalshi.Outcome
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO instruments (
			id, identity_key, asset_class, primary_venue, currency,
			tick_size, lot_size, multiplier, settlement_method, status
		) VALUES ($1,$2,$3,$4,'USD',0.01,1,1,$5,'active');
		INSERT INTO venue_contracts (
			id, instrument_id, venue, contract_id, currency, tick_size,
			lot_size, multiplier, settlement_method, valid_from, valid_to, metadata
		) VALUES ($6,$1,$4,$7,'USD',0.01,1,1,$5,$8::TIMESTAMPTZ - interval '1 day',
			$8::TIMESTAMPTZ + interval '1 day',$9::JSONB);
		INSERT INTO quote_snapshots (
			id, instrument_id, venue_contract_id, provider, venue, source,
			observation_namespace, observation_id, exchange_at, received_at,
			available_at, bid, ask, bid_depth_count, ask_depth_count
		) VALUES ($10,$1,$6,$4,$4,'migration-fixture',$11,$12,
			$8::TIMESTAMPTZ - interval '3 seconds',$8::TIMESTAMPTZ - interval '2 seconds',
			$8::TIMESTAMPTZ - interval '1 second',0.41,0.43,0,0)`,
		fixture.Common.InstrumentID, "venue:migration:"+fixture.Common.InstrumentID.String(), assetClass,
		string(provider), settlement, fixture.Common.VenueContractID, fixture.ContractID,
		fixture.Common.DecisionAt, metadata, fixture.Common.QuoteSnapshotID,
		"quotes/"+string(provider), "quote-"+fixture.Common.QuoteSnapshotID.String(),
	); err != nil {
		t.Fatalf("seed %s venue fixture: %v", provider, err)
	}
	return fixture
}

func insertVenueMigrationOrder(
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture venueAdapterMigrationFixture,
	intentID uuid.UUID,
	key, policyVersion, timeInForce string,
) (uuid.UUID, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	orderID, err := insertVenueMigrationOrderInTx(ctx, tx, fixture, intentID, key, policyVersion, timeInForce)
	if err != nil {
		return uuid.Nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	return orderID, nil
}

func insertVenueMigrationOrderInTx(
	ctx context.Context,
	tx pgx.Tx,
	fixture venueAdapterMigrationFixture,
	intentID uuid.UUID,
	key, policyVersion, timeInForce string,
) (uuid.UUID, error) {
	orderID := economicid.DeterministicUUID("execution-order", intentID.String(), "order-"+key)
	routedAt := fixture.Common.DecisionAt.Add(3 * time.Second)
	if _, err := tx.Exec(ctx, `INSERT INTO execution_orders (
		id, intent_id, account_id, instrument_id, idempotency_key, client_order_id,
		side, order_type, time_in_force, quantity, limit_price, venue,
		venue_contract_id, route_quote_snapshot_id, routed_at, policy_kind,
		policy_version, created_at
	) VALUES ($1,$2,$3,$4,$5,$1::TEXT,'buy','limit',$6,8,$7,$8,$9,$10,$11,'venue',$12,$11)`,
		orderID, intentID, fixture.Common.AccountID, fixture.Common.InstrumentID, "order-"+key,
		timeInForce, fixture.LimitPrice, string(fixture.Provider), fixture.Common.VenueContractID,
		fixture.Common.QuoteSnapshotID, routedAt, policyVersion,
	); err != nil {
		return uuid.Nil, err
	}
	evidence := []byte(`{"route":"test"}`)
	sourceEventID := "route-" + key
	eventID := economicid.DeterministicUUID(
		"execution-lifecycle-event", intentID.String(), "ordinary", "router",
		"lifecycle/test", sourceEventID, "",
	)
	if _, err := tx.Exec(ctx, `INSERT INTO execution_lifecycle_events (
		id, intent_id, order_id, kind, observation_class, prior_state, next_state,
		account_id, environment, origin_type, origin_id, strategy_version_id,
		policy_kind, policy_version, quantity_delta, quote_snapshot_id, source,
		source_namespace, source_event_id, source_at, received_at, actor,
		reason_code, evidence_bytes, evidence_sha256, evidence, created_at
	) VALUES ($1,$2,$3,'order_routed','ordinary','risk_approved','routed',$4,'paper_scored',
		'strategy_version','strategy-version-1','strategy-version-1','venue',$5,8,$6,'router',
		'lifecycle/test',$7,$8,$8,'router','order_routed',$9,$10,convert_from($9,'UTF8')::JSONB,$8)`,
		eventID, intentID, orderID, fixture.Common.AccountID, policyVersion,
		fixture.Common.QuoteSnapshotID, sourceEventID, routedAt, evidence, migrationLifecycleSHA(evidence),
	); err != nil {
		return uuid.Nil, err
	}
	return orderID, nil
}

func venueObservationInput(
	fixture venueAdapterMigrationFixture,
	intentID, orderID uuid.UUID,
	policyVersion, sourceEventID string,
) venue.ObservationInput {
	providerPrice := decimal.RequireFromString("0.58")
	return venue.ObservationInput{
		AccountID: fixture.Common.AccountID, IntentID: intentID, OrderID: orderID,
		VenueContractID: fixture.Common.VenueContractID, Provider: fixture.Provider,
		Venue: string(fixture.Provider), PolicyVersion: policyVersion, Kind: venue.ObservationFill,
		ProviderState: "fill", MappedOutcome: venue.OutcomeFill, ExternalOrderID: "external-" + orderID.String(),
		ClientOrderID: orderID.String(), ProviderContractID: fixture.ContractID,
		CanonicalOutcome: fixture.Outcome, ProviderBookSide: "ask", ProviderAction: "buy",
		ProviderPrice: &providerPrice, IdentityKind: venue.SourceIdentityProvider,
		SourceNamespace: "kalshi/portfolio/fills", SourceEventID: sourceEventID, SourceRevision: "1",
		SourceAt: venueMigrationTime().Add(2 * time.Hour), ReceivedAt: venueMigrationTime().Add(2*time.Hour + time.Second),
		RawPayload: json.RawMessage(`{"fill_id":"` + sourceEventID + `","price":"0.58"}`),
		CreatedAt:  venueMigrationTime().Add(2*time.Hour + 2*time.Second),
	}
}

func venueMigrationObservation(
	t *testing.T,
	fixture venueAdapterMigrationFixture,
	intentID, orderID uuid.UUID,
	policyVersion, sourceEventID string,
) *venue.Observation {
	t.Helper()
	observation, err := venue.NewObservation(venueObservationInput(fixture, intentID, orderID, policyVersion, sourceEventID))
	if err != nil {
		t.Fatal(err)
	}
	return observation
}

func venueMigrationAcknowledgement(
	t *testing.T,
	fixture venueAdapterMigrationFixture,
	intentID, orderID uuid.UUID,
	policyVersion, sourceEventID string,
) *venue.Observation {
	t.Helper()
	providerPrice := decimal.RequireFromString(fixture.LimitPrice)
	if fixture.Outcome == "no" {
		providerPrice = decimal.NewFromInt(1).Sub(providerPrice)
	}
	input := venueObservationInput(fixture, intentID, orderID, policyVersion, sourceEventID)
	input.Kind = venue.ObservationOrderSnapshot
	input.ProviderState = "resting"
	input.MappedOutcome = venue.OutcomeAcknowledge
	input.SourceNamespace = "kalshi/portfolio/order-snapshots"
	input.ProviderPrice = &providerPrice
	input.ProviderBookSide = "bid"
	if fixture.Outcome == "no" {
		input.ProviderBookSide = "ask"
	}
	input.RawPayload = json.RawMessage(`{"order_id":"external-` + orderID.String() + `","status":"resting"}`)
	observation, err := venue.NewObservation(input)
	if err != nil {
		t.Fatal(err)
	}
	return observation
}

type venueObservationExecer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

const venueMigrationObservationInsertSQL = `INSERT INTO venue_observations (
	id, account_id, intent_id, order_id, binding_id, venue_contract_id,
	provider, venue, policy_version, kind, provider_state, mapped_outcome,
	external_order_id, client_order_id, provider_contract_id, canonical_outcome,
	provider_book_side, provider_action, provider_price, identity_kind,
	source_namespace, source_event_id, source_revision, source_at, received_at,
	raw_bytes, raw_sha256, raw_json, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,
	$19::NUMERIC,$20,$21,$22,$23,$24,$25,$26,$27,convert_from($26,'UTF8')::JSONB,$28)`

func insertVenueMigrationObservation(ctx context.Context, database venueObservationExecer, observation *venue.Observation) error {
	_, err := database.Exec(ctx, venueMigrationObservationInsertSQL, venueMigrationObservationArgs(observation)...)
	return err
}

func venueMigrationObservationArgs(observation *venue.Observation) []any {
	var providerPrice any
	if observation.ProviderPrice != nil {
		providerPrice = observation.ProviderPrice.String()
	}
	return []any{
		observation.ID, observation.AccountID, observation.IntentID, observation.OrderID, observation.BindingID,
		observation.VenueContractID, observation.Provider, observation.Venue, observation.PolicyVersion,
		observation.Kind, observation.ProviderState, observation.MappedOutcome, observation.ExternalOrderID,
		observation.ClientOrderID, observation.ProviderContractID, observation.CanonicalOutcome,
		observation.ProviderBookSide, observation.ProviderAction, providerPrice, observation.IdentityKind,
		observation.SourceNamespace, observation.SourceEventID, observation.SourceRevision,
		observation.SourceAt, observation.ReceivedAt, []byte(observation.RawPayload), observation.PayloadSHA256,
		observation.CreatedAt,
	}
}

func insertVenueMigrationAcknowledgement(
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture venueAdapterMigrationFixture,
	intentID, orderID uuid.UUID,
	policyVersion string,
	observation *venue.Observation,
) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	bindingID := economicid.DeterministicUUID("execution-order-binding", orderID.String())
	if _, err := tx.Exec(ctx, `INSERT INTO execution_order_bindings (
		id, order_id, account_id, venue, external_order_id, created_at
	) VALUES ($1,$2,$3,$4,$5,$6)`, bindingID, orderID, fixture.Common.AccountID,
		fixture.Provider, observation.ExternalOrderID, observation.ReceivedAt,
	); err != nil {
		return err
	}
	eventID := economicid.DeterministicUUID(
		"execution-lifecycle-event", intentID.String(), "ordinary", string(fixture.Provider),
		observation.SourceNamespace, observation.SourceEventID, "",
	)
	if _, err := tx.Exec(ctx, `INSERT INTO execution_lifecycle_events (
		id, intent_id, order_id, binding_id, kind, observation_class, prior_state,
		next_state, account_id, environment, origin_type, origin_id,
		strategy_version_id, policy_kind, policy_version, quantity_delta, source,
		source_namespace, source_event_id, source_revision, source_at, received_at,
		actor, reason_code, evidence_bytes, evidence_sha256, evidence, created_at
	) VALUES ($1,$2,$3,$4,'order_working','ordinary','routed','working',$5,'paper_scored',
		'strategy_version','strategy-version-1','strategy-version-1','venue',$6,8,$7,$8,$9,$10,
		$11,$12,'venue-adapter','provider_acknowledged',$13,$14,convert_from($13,'UTF8')::JSONB,$12)`,
		eventID, intentID, orderID, bindingID, fixture.Common.AccountID, policyVersion,
		fixture.Provider, observation.SourceNamespace, observation.SourceEventID, observation.SourceRevision,
		observation.SourceAt, observation.ReceivedAt, []byte(observation.RawPayload), observation.PayloadSHA256,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func newVenueAdapterMigrationPool(t *testing.T) (context.Context, *pgxpool.Pool, commonExecutionLifecycleMigrationFixture) {
	t.Helper()
	ctx, pool, fixture := newSimulationPolicyMigrationPool(t)
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000073_venue_adapter_observations.up.sql")); err != nil {
		t.Fatalf("apply migration 73: %v", err)
	}
	return ctx, pool, fixture
}

func venueMigrationArtifact(t *testing.T, provider venue.Provider) venue.PolicyArtifact {
	t.Helper()
	policy, err := venue.ReviewedPolicy(provider)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := policy.NewArtifact(venueMigrationTime())
	if err != nil {
		t.Fatal(err)
	}
	return *artifact
}

func forgedVenueMigrationArtifact(canonical []byte, provider venue.Provider) venue.PolicyArtifact {
	digestBytes := sha256.Sum256(canonical)
	digest := hex.EncodeToString(digestBytes[:])
	version := venue.PolicySchemaV1 + "@sha256:" + digest
	return venue.PolicyArtifact{
		ID:     economicid.DeterministicUUID("venue-adapter-policy-artifact", version),
		Schema: venue.PolicySchemaV1, Provider: provider, Venue: string(provider),
		Version: version, SHA256: digest, CanonicalBytes: canonical, CreatedAt: venueMigrationTime(),
	}
}

func insertVenueMigrationArtifact(ctx context.Context, database venueObservationExecer, artifact venue.PolicyArtifact) error {
	_, err := database.Exec(ctx, `INSERT INTO venue_adapter_policy_artifacts (
		id, schema_name, provider, venue, policy_version, sha256,
		canonical_bytes, canonical_json, created_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,convert_from($7,'UTF8')::JSONB,$8)`,
		artifact.ID, artifact.Schema, artifact.Provider, artifact.Venue, artifact.Version,
		artifact.SHA256, []byte(artifact.CanonicalBytes), artifact.CreatedAt,
	)
	return err
}

func deepCopyMigrationJSON(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var copied map[string]any
	if err := json.Unmarshal(encoded, &copied); err != nil {
		t.Fatal(err)
	}
	return copied
}

func venueMigrationTime() time.Time {
	return time.Date(2026, time.August, 15, 21, 0, 0, 123456000, time.UTC)
}
