package automation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	prediction "github.com/PatrickFanella/get-rich-quick/internal/execution/prediction"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

var kalshiSettlementSpec = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeCron, Cron: "*/5 * * * *"}

func (o *JobOrchestrator) registerKalshiSettlementJob() {
	if o.deps.KalshiCatalog == nil || o.deps.PredictionSettler == nil {
		return
	}
	o.Register("kalshi_settlement", "Cash-settle resolved Kalshi paper contracts and journal outcomes", kalshiSettlementSpec, o.kalshiSettlement)
}

func (o *JobOrchestrator) kalshiSettlement(ctx context.Context) error {
	_, ok := o.jobs["kalshi_settlement"]
	if !ok {
		return fmt.Errorf("kalshi_settlement: job not registered")
	}
	type projectedMarket struct {
		ticker, winner, fingerprint string
		count                       int
	}
	var fetched, resolved, wouldSettleMarkets, wouldSettleDecisions int
	projected := make([]projectedMarket, 0, 8)
	projection := make([]string, 0, 8)
	threshold := o.deps.KalshiSettlementThreshold
	if threshold <= 0 {
		threshold = 20
	}
	pending, err := o.deps.PredictionSettler.PendingMarkets(ctx, domain.MarketTypeKalshi)
	if err != nil {
		if _, persistErr := o.kalshiSettlementRecordFailure(ctx, threshold, fetched, resolved, wouldSettleMarkets, wouldSettleDecisions, err.Error()); persistErr != nil {
			return persistErr
		}
		o.recordKalshiSettlementMetrics(false, true)
		return fmt.Errorf("kalshi_settlement: load pending markets: %w", err)
	}
	fetched += len(pending)
	for _, ticker := range pending {
		market, err := o.deps.KalshiCatalog.GetMarket(ctx, ticker)
		if err != nil {
			if _, persistErr := o.kalshiSettlementRecordFailure(ctx, threshold, fetched, resolved, wouldSettleMarkets, wouldSettleDecisions, err.Error()); persistErr != nil {
				return persistErr
			}
			o.recordKalshiSettlementMetrics(false, true)
			return fmt.Errorf("kalshi_settlement: get market %s: %w", ticker, err)
		}
		if market == nil {
			continue
		}
		winner := strings.ToUpper(strings.TrimSpace(market.Result))
		if winner != "YES" && winner != "NO" {
			continue
		}
		resolved++
		preview, err := o.deps.PredictionSettler.SettlePreview(ctx, domain.MarketTypeKalshi, market.Ticker)
		if err != nil {
			if _, persistErr := o.kalshiSettlementRecordFailure(ctx, threshold, fetched, resolved, wouldSettleMarkets, wouldSettleDecisions, err.Error()); persistErr != nil {
				return persistErr
			}
			o.recordKalshiSettlementMetrics(false, true)
			return fmt.Errorf("kalshi_settlement: preview market %s: %w", market.Ticker, err)
		}
		if preview.Count > 0 {
			wouldSettleMarkets++
			wouldSettleDecisions += preview.Count
		}
		previewFingerprint := settlementPreviewFingerprint(preview, winner)
		projected = append(projected, projectedMarket{ticker: strings.ToUpper(strings.TrimSpace(market.Ticker)), winner: winner, count: preview.Count, fingerprint: previewFingerprint})
		projection = append(projection, previewFingerprint)
	}
	fingerprint := settlementProjectionFingerprint(projection)
	liveRequested := o.deps.KalshiSettlementEnabled && !o.deps.KalshiSettlementDryRun
	liveEligible := false
	if liveRequested {
		state, err := o.kalshiSettlementGateState(ctx)
		if err != nil {
			o.recordKalshiSettlementMetrics(false, true)
			return err
		}
		liveEligible = state != nil && state.Eligible && state.ProjectionFingerprint == fingerprint
	}
	if liveEligible {
		for _, pm := range projected {
			preview, err := o.deps.PredictionSettler.SettlePreview(ctx, domain.MarketTypeKalshi, pm.ticker)
			if err != nil {
				if _, persistErr := o.kalshiSettlementRecordFailure(ctx, threshold, fetched, resolved, wouldSettleMarkets, wouldSettleDecisions, err.Error()); persistErr != nil {
					return persistErr
				}
				o.recordKalshiSettlementMetrics(false, false)
				return fmt.Errorf("kalshi_settlement: refresh preview market %s: %w", pm.ticker, err)
			}
			if preview.Count != pm.count || settlementPreviewFingerprint(preview, pm.winner) != pm.fingerprint {
				mismatch := fmt.Errorf("kalshi_settlement: preview changed before settlement for %s", pm.ticker)
				if _, persistErr := o.kalshiSettlementRecordFailure(ctx, threshold, fetched, resolved, wouldSettleMarkets, wouldSettleDecisions, mismatch.Error()); persistErr != nil {
					return persistErr
				}
				o.recordKalshiSettlementMetrics(false, false)
				return mismatch
			}
			if _, err := o.deps.PredictionSettler.SettleDecisions(ctx, domain.MarketTypeKalshi, pm.ticker, pm.winner, time.Now().UTC(), preview.DecisionIDs); err != nil {
				if _, persistErr := o.kalshiSettlementRecordFailure(ctx, threshold, fetched, resolved, wouldSettleMarkets, wouldSettleDecisions, err.Error()); persistErr != nil {
					return persistErr
				}
				o.recordKalshiSettlementMetrics(false, false)
				return fmt.Errorf("kalshi_settlement: live settle market %s: %w", pm.ticker, err)
			}
		}
	}
	dryRun := !liveEligible
	if dryRun {
		if _, err := o.kalshiSettlementRecordSuccess(ctx, threshold, fetched, resolved, wouldSettleMarkets, wouldSettleDecisions, fingerprint); err != nil {
			return err
		}
	}
	o.SetLastSummary("kalshi_settlement", map[string]int{"dry_run": btoi(dryRun), "fetched": fetched, "resolved": resolved, "would_settle_markets": wouldSettleMarkets, "would_settle_decisions": wouldSettleDecisions})
	o.recordKalshiSettlementMetrics(true, dryRun)
	return nil
}

func (o *JobOrchestrator) kalshiSettlementGateState(ctx context.Context) (*domain.KalshiSettlementGateState, error) {
	if o.deps.KalshiSettlementGateRepo == nil {
		o.kalshiGateUnhealthy = true
		return nil, fmt.Errorf("kalshi_settlement: gate unavailable")
	}
	state, err := o.deps.KalshiSettlementGateRepo.Get(ctx, "kalshi_settlement")
	if err != nil {
		o.kalshiGateUnhealthy = true
		return nil, fmt.Errorf("kalshi_settlement: load gate: %w", err)
	}
	o.kalshiGateUnhealthy = false
	o.updateKalshiSettlementGateStatus(state)
	return state, nil
}

func (o *JobOrchestrator) recordKalshiSettlementMetrics(success, dryRun bool) {
	if o.metrics == nil {
		return
	}
	result := "failure"
	if success {
		result = "success"
	}
	if dryRun {
		o.metrics.RecordKalshiSettlementDryRun(result)
	}
	o.metrics.RecordKalshiSettlementOutcome(result)
}

func btoi(v bool) int {
	if v {
		return 1
	}
	return 0
}

func settlementProjectionFingerprint(parts []string) string {
	if len(parts) == 0 {
		parts = []string{"<empty>"}
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func settlementPreviewFingerprint(preview *prediction.SettlementPreview, winner string) string {
	parts := []string{strings.ToUpper(strings.TrimSpace(preview.GetInstrument())), strings.ToUpper(strings.TrimSpace(winner))}
	for _, id := range preview.GetDecisionIDs() {
		parts = append(parts, id.String())
	}
	return settlementProjectionFingerprint(parts)
}

func (o *JobOrchestrator) kalshiSettlementRecordSuccess(ctx context.Context, threshold, fetched, resolved, wouldSettleMarkets, wouldSettleDecisions int, fingerprint string) (*domain.KalshiSettlementGateState, error) {
	if o.deps.KalshiSettlementGateRepo == nil {
		o.kalshiGateUnhealthy = true
		return nil, fmt.Errorf("kalshi_settlement: gate unavailable")
	}
	state, err := o.deps.KalshiSettlementGateRepo.RecordSuccess(ctx, "kalshi_settlement", threshold, fetched, resolved, wouldSettleMarkets, wouldSettleDecisions, fingerprint, time.Now().UTC())
	if err != nil {
		o.kalshiGateUnhealthy = true
		return nil, fmt.Errorf("kalshi_settlement: persist gate success: %w", err)
	}
	o.kalshiGateUnhealthy = false
	o.updateKalshiSettlementGateStatus(state)
	return state, nil
}

func (o *JobOrchestrator) kalshiSettlementRecordFailure(ctx context.Context, threshold, fetched, resolved, wouldSettleMarkets, wouldSettleDecisions int, lastError string) (*domain.KalshiSettlementGateState, error) {
	if o.deps.KalshiSettlementGateRepo == nil {
		o.kalshiGateUnhealthy = true
		return nil, nil
	}
	state, err := o.deps.KalshiSettlementGateRepo.RecordFailure(ctx, "kalshi_settlement", threshold, fetched, resolved, wouldSettleMarkets, wouldSettleDecisions, time.Now().UTC(), lastError)
	if err != nil {
		o.kalshiGateUnhealthy = true
		return nil, fmt.Errorf("kalshi_settlement: persist gate failure: %w", err)
	}
	o.kalshiGateUnhealthy = true
	o.updateKalshiSettlementGateStatus(state)
	return state, nil
}

func (o *JobOrchestrator) updateKalshiSettlementGateStatus(state *domain.KalshiSettlementGateState) {
	job, ok := o.jobs["kalshi_settlement"]
	if !ok {
		return
	}
	job.mu.Lock()
	job.SettlementGate = settlementGateStatusFromState(state)
	job.mu.Unlock()
}
