package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRiskMonitor_KillSwitchInactive(t *testing.T) {
	re := &mockRiskEngine{}
	mon := &riskMonitor{
		riskEngine:   re,
		pollInterval: 10 * time.Millisecond,
		logger:       testLogger(),
	}

	ctx, cancel := mon.monitorContext(context.Background())
	defer cancel()

	// Let a few poll cycles run.
	time.Sleep(50 * time.Millisecond)

	select {
	case <-ctx.Done():
		t.Fatal("context should not be cancelled when kill switch is inactive")
	default:
	}
}

func TestRiskMonitor_KillSwitchActiveCancelsContext(t *testing.T) {
	re := &mockRiskEngine{killSwitchActive: true}
	mon := &riskMonitor{
		riskEngine:   re,
		pollInterval: 10 * time.Millisecond,
		logger:       testLogger(),
	}

	ctx, cancel := mon.monitorContext(context.Background())
	defer cancel()

	select {
	case <-ctx.Done():
		// Success: context was cancelled.
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for context cancellation after kill switch activation")
	}
}

func TestRiskMonitor_ErrorKeepsContextAlive(t *testing.T) {
	re := &mockRiskEngine{killSwitchErr: errors.New("network error")}
	mon := &riskMonitor{
		riskEngine:   re,
		pollInterval: 10 * time.Millisecond,
		logger:       testLogger(),
	}

	ctx, cancel := mon.monitorContext(context.Background())
	defer cancel()

	// Let a few poll cycles run.
	time.Sleep(50 * time.Millisecond)

	select {
	case <-ctx.Done():
		t.Fatal("context should not be cancelled on poll errors")
	default:
	}
}

func TestRiskMonitor_ParentCancelStopsMonitor(t *testing.T) {
	re := &mockRiskEngine{}
	mon := &riskMonitor{
		riskEngine:   re,
		pollInterval: 10 * time.Millisecond,
		logger:       testLogger(),
	}

	parent, parentCancel := context.WithCancel(context.Background())
	ctx, cancel := mon.monitorContext(parent)
	defer cancel()

	parentCancel()

	select {
	case <-ctx.Done():
		// Success: derived context cancelled when parent cancelled.
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for context cancellation after parent cancel")
	}
}
