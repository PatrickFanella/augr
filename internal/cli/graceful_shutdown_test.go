package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/api"
	"github.com/PatrickFanella/get-rich-quick/internal/config"
)

// --------------------------------------------------------------------------
// Test doubles
// --------------------------------------------------------------------------

// mockScheduler is a test double for SchedulerLifecycle.
type mockScheduler struct {
	startErr     error
	started      atomic.Bool
	stopped      atomic.Bool
	inFlight     atomic.Int32
	stopBlockCh  chan struct{} // if non-nil Stop() blocks until this is closed
	stopCalledCh chan struct{} // closed once Stop() is entered; always set
	onceStop     sync.Once
}

func newMockScheduler() *mockScheduler {
	return &mockScheduler{stopCalledCh: make(chan struct{})}
}

func (m *mockScheduler) Start() error {
	m.started.Store(true)
	return m.startErr
}

func (m *mockScheduler) Stop() {
	m.onceStop.Do(func() { close(m.stopCalledCh) })
	if m.stopBlockCh != nil {
		<-m.stopBlockCh
	}
	m.stopped.Store(true)
}

func (m *mockScheduler) InFlightCount() int {
	return int(m.inFlight.Load())
}

// callRecorder records names of functions in the order they are invoked.
// It is safe for concurrent use.
type callRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *callRecorder) record(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name)
}

func (r *callRecorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

// recordingScheduler delegates to an inner SchedulerLifecycle but runs onStop
// immediately before calling Stop() on the inner scheduler.
type recordingScheduler struct {
	inner  SchedulerLifecycle
	onStop func()
}

func (s *recordingScheduler) Start() error { return s.inner.Start() }

func (s *recordingScheduler) Stop() {
	if s.onStop != nil {
		s.onStop()
	}
	s.inner.Stop()
}

func (s *recordingScheduler) InFlightCount() int {
	return s.inner.InFlightCount()
}

// --------------------------------------------------------------------------
// blockingServe is a helper that returns functions suitable for
// runServerLifecycle/runServeLifecycle.  serveStarted is closed once
// serve() is entered; shutdown() unblocks serve() and returns immediately.
// --------------------------------------------------------------------------
type blockingServe struct {
	started chan struct{}
	done    chan struct{}
	once    sync.Once
}

func newBlockingServe() *blockingServe {
	return &blockingServe{
		started: make(chan struct{}),
		done:    make(chan struct{}),
	}
}

func (b *blockingServe) serve() error {
	b.once.Do(func() { close(b.started) })
	<-b.done
	return http.ErrServerClosed
}

func (b *blockingServe) shutdown(_ context.Context) error {
	close(b.done)
	return nil
}

// --------------------------------------------------------------------------
// runServerLifecycle tests
// --------------------------------------------------------------------------

// TestRunServerLifecycle_CallsShutdownOnContextCancel verifies that
// runServerLifecycle calls shutdown when the parent context is cancelled.
func TestRunServerLifecycle_CallsShutdownOnContextCancel(t *testing.T) {
	t.Parallel()

	bs := newBlockingServe()
	shutdownCalled := make(chan struct{}, 1)
	shutdown := func(ctx context.Context) error {
		shutdownCalled <- struct{}{}
		return bs.shutdown(ctx)
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- runServerLifecycle(ctx, bs.serve, shutdown) }()

	// Wait for serve() to be entered before triggering shutdown.
	select {
	case <-bs.started:
	case <-time.After(3 * time.Second):
		t.Fatal("serve() was not entered within 3 s")
	}

	cancel()

	select {
	case <-shutdownCalled:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown was not called after context cancellation")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runServerLifecycle returned %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for runServerLifecycle to return")
	}
}

// TestRunServerLifecycle_ReturnsServerErrorWhenServeFails verifies that a
// startup error from serve() is surfaced by runServerLifecycle.
func TestRunServerLifecycle_ReturnsServerErrorWhenServeFails(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("listen: address already in use")
	serve := func() error { return wantErr }
	shutdown := func(_ context.Context) error { return nil }

	ctx := context.Background()
	err := runServerLifecycle(ctx, serve, shutdown)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

// TestRunServerLifecycle_HonorsShutdownTimeout verifies that runServerLifecycle
// passes a context with a deadline to the shutdown function.
func TestRunServerLifecycle_HonorsShutdownTimeout(t *testing.T) {
	t.Parallel()

	bs := newBlockingServe()
	var shutdownCtx context.Context
	shutdown := func(ctx context.Context) error {
		shutdownCtx = ctx
		return bs.shutdown(ctx)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runServerLifecycle(ctx, bs.serve, shutdown) }()

	select {
	case <-bs.started:
	case <-time.After(3 * time.Second):
		t.Fatal("serve() was not entered within 3 s")
	}

	cancel()
	<-done

	if _, ok := shutdownCtx.Deadline(); !ok {
		t.Fatal("shutdown context has no deadline; expected a graceful-shutdown timeout")
	}
}

func TestRunServerLifecycle_CallsShutdownHookOnContextCancel(t *testing.T) {
	t.Parallel()

	bs := newBlockingServe()
	hookCalled := make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runServerLifecycleWithHook(ctx, bs.serve, bs.shutdown, func() {
			hookCalled <- struct{}{}
		})
	}()

	select {
	case <-bs.started:
	case <-time.After(3 * time.Second):
		t.Fatal("serve() was not entered within 3 s")
	}

	cancel()

	select {
	case <-hookCalled:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown hook was not called after context cancellation")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runServerLifecycleWithHook returned %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for runServerLifecycleWithHook to return")
	}
}

func TestShutdownGuard_LogsStructuredLifecycleMessages(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	guard := newShutdownGuard(logger, time.Minute, func(int) {})

	guard.Begin(syscall.SIGTERM, 3)
	guard.Finish()

	entries := parseLogEntries(t, buf.String())
	if len(entries) != 3 {
		t.Fatalf("expected 3 log entries, got %d", len(entries))
	}

	assertLogEntry(t, entries[0], "INFO", "shutdown initiated", map[string]any{
		inFlightPipelineRunsKey: float64(3),
		shutdownSignalKey:       syscall.SIGTERM.String(),
	})
	assertLogEntry(t, entries[1], "INFO", "waiting for in-flight pipeline runs", map[string]any{
		inFlightPipelineRunsKey: float64(3),
	})
	assertLogEntry(t, entries[2], "INFO", "shutdown complete", nil)
}

func TestShutdownGuard_ForcesExitAfterTimeout(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	exitCalled := make(chan int, 1)
	guard := newShutdownGuard(logger, 20*time.Millisecond, func(code int) {
		exitCalled <- code
	})

	guard.Begin(nil, 2)

	select {
	case code := <-exitCalled:
		if code != forcedShutdownExitCode {
			t.Fatalf("exit code = %d, want %d", code, forcedShutdownExitCode)
		}
	case <-time.After(time.Second):
		t.Fatal("forced shutdown exit was not triggered")
	}

	entries := parseLogEntries(t, buf.String())
	if len(entries) < 3 {
		t.Fatalf("expected at least 3 log entries, got %d", len(entries))
	}
	assertLogEntry(t, entries[len(entries)-1], "ERROR", "shutdown timed out; forcing exit", map[string]any{
		inFlightPipelineRunsKey: float64(2),
	})
}

// --------------------------------------------------------------------------
// Scheduler lifecycle ordering tests
// --------------------------------------------------------------------------

// runServeLifecycle mirrors the deferred ordering in the serve command RunE:
//
//	defer cleanup()             // registered first → runs last
//	// signal context registered before scheduler start (not modelled here)
//	defer sched.Stop()          // registered after cleanup → runs before cleanup()
//	runServerLifecycle(...)
//
// It executes the same sequence so that tests can verify the ordering without
// requiring a real config or a running HTTP server.
func runServeLifecycle(
	ctx context.Context,
	sched SchedulerLifecycle,
	cleanup func(),
	serve func() error,
	shutdown func(context.Context) error,
) error {
	defer cleanup()
	if sched != nil {
		if err := sched.Start(); err != nil {
			return err
		}
		defer sched.Stop()
	}
	return runServerLifecycle(ctx, serve, shutdown)
}

// TestGracefulShutdown_SchedulerStartsBeforeServe verifies that Start() is
// called before the HTTP server begins serving.
func TestGracefulShutdown_SchedulerStartsBeforeServe(t *testing.T) {
	t.Parallel()

	sched := newMockScheduler()
	bs := newBlockingServe()

	serve := func() error {
		if !sched.started.Load() {
			t.Error("serve was called but scheduler Start() had not been called yet")
		}
		return bs.serve()
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runServeLifecycle(ctx, sched, func() {}, serve, bs.shutdown) }()

	select {
	case <-bs.started:
	case <-time.After(3 * time.Second):
		t.Fatal("serve() was not entered within 3 s")
	}

	cancel()
	<-done
}

// TestGracefulShutdown_SchedulerStopsBeforeDBClose verifies that Stop() is
// called on the scheduler before the cleanup (DB-close) function runs.
// This ensures in-flight pipeline runs can persist their terminal status while
// the connection pool is still open.
func TestGracefulShutdown_SchedulerStopsBeforeDBClose(t *testing.T) {
	t.Parallel()

	rec := &callRecorder{}
	sched := &recordingScheduler{
		inner:  newMockScheduler(),
		onStop: func() { rec.record("scheduler.Stop") },
	}
	cleanup := func() { rec.record("db.Close") }
	bs := newBlockingServe()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runServeLifecycle(ctx, sched, cleanup, bs.serve, bs.shutdown)
	}()

	// Wait for serve to be entered, then cancel (simulate SIGTERM).
	select {
	case <-bs.started:
	case <-time.After(3 * time.Second):
		t.Fatal("serve() was not entered within 3 s")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("lifecycle did not complete")
	}

	calls := rec.all()
	if len(calls) < 2 {
		t.Fatalf("expected 2 recorded calls, got %v", calls)
	}
	if calls[0] != "scheduler.Stop" {
		t.Errorf("calls[0] = %q, want %q (scheduler.Stop must precede db.Close)", calls[0], "scheduler.Stop")
	}
	if calls[1] != "db.Close" {
		t.Errorf("calls[1] = %q, want %q", calls[1], "db.Close")
	}
}

// TestGracefulShutdown_ActiveJobsWaitedForBeforeDBClose verifies that the
// shutdown sequence waits for in-flight scheduler jobs to complete before the
// DB pool is closed.  A slow Stop() simulates a pipeline run persisting its
// terminal status to the database.
func TestGracefulShutdown_ActiveJobsWaitedForBeforeDBClose(t *testing.T) {
	t.Parallel()

	rec := &callRecorder{}
	unblockScheduler := make(chan struct{})

	inner := &mockScheduler{
		stopCalledCh: make(chan struct{}),
		stopBlockCh:  unblockScheduler,
	}
	sched := &recordingScheduler{
		inner:  inner,
		onStop: func() { rec.record("scheduler.Stop") },
	}
	cleanup := func() { rec.record("db.Close") }
	bs := newBlockingServe()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runServeLifecycle(ctx, sched, cleanup, bs.serve, bs.shutdown)
	}()

	select {
	case <-bs.started:
	case <-time.After(3 * time.Second):
		t.Fatal("serve() was not entered within 3 s")
	}
	cancel()

	// Wait until Stop() has been entered (job is still running).
	select {
	case <-inner.stopCalledCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for scheduler Stop() to be called")
	}

	// DB must NOT be closed yet: the scheduler is still draining.
	for _, c := range rec.all() {
		if c == "db.Close" {
			t.Fatal("db.Close was called before scheduler.Stop() finished; in-flight runs could lose their status update")
		}
	}

	// Unblock the scheduler (in-flight job finishes, terminal status persisted).
	close(unblockScheduler)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("lifecycle did not complete after unblocking scheduler")
	}

	calls := rec.all()
	if len(calls) < 2 || calls[0] != "scheduler.Stop" || calls[1] != "db.Close" {
		t.Errorf("call order = %v, want [scheduler.Stop, db.Close]", calls)
	}
}

// TestGracefulShutdown_NilSchedulerIsHandled verifies that a nil
// SchedulerLifecycle does not panic and the serve lifecycle completes normally.
func TestGracefulShutdown_NilSchedulerIsHandled(t *testing.T) {
	t.Parallel()

	cleanupCalled := make(chan struct{}, 1)
	cleanup := func() { cleanupCalled <- struct{}{} }
	bs := newBlockingServe()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runServeLifecycle(ctx, nil, cleanup, bs.serve, bs.shutdown)
	}()

	select {
	case <-bs.started:
	case <-time.After(3 * time.Second):
		t.Fatal("serve() was not entered within 3 s")
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runServeLifecycle with nil scheduler = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("lifecycle with nil scheduler did not complete")
	}

	select {
	case <-cleanupCalled:
	default:
		t.Fatal("cleanup was not called")
	}
}

// TestGracefulShutdown_SchedulerStartErrorPreventsServe verifies that if
// Start() returns an error the lifecycle aborts before serving HTTP traffic.
func TestGracefulShutdown_SchedulerStartErrorPreventsServe(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("cron engine unavailable")
	sched := &mockScheduler{
		startErr:     wantErr,
		stopCalledCh: make(chan struct{}),
	}

	serveCalled := false
	serve := func() error { serveCalled = true; return nil }
	shutdown := func(_ context.Context) error { return nil }

	err := runServeLifecycle(context.Background(), sched, func() {}, serve, shutdown)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if serveCalled {
		t.Fatal("serve was called even though scheduler Start() failed")
	}
}

func parseLogEntries(t *testing.T, logs string) []map[string]any {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(logs), "\n")
	entries := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("unmarshal log entry %q: %v", line, err)
		}
		entries = append(entries, entry)
	}
	return entries
}

func assertLogEntry(t *testing.T, entry map[string]any, level, msg string, wantFields map[string]any) {
	t.Helper()

	if got := entry["level"]; got != level {
		t.Fatalf("log level = %v, want %s", got, level)
	}
	if got := entry["msg"]; got != msg {
		t.Fatalf("log msg = %v, want %s", got, msg)
	}
	for key, want := range wantFields {
		if got := entry[key]; got != want {
			t.Fatalf("log field %q = %v, want %v", key, got, want)
		}
	}
}

// --------------------------------------------------------------------------
// newAPIServer integration plumbing test
// --------------------------------------------------------------------------

// TestNewAPIServerDependencyTypeContractIsCorrect is a compile-time and
// runtime check that the Dependencies.NewAPIServer function signature matches
// the four-value return expected by the graceful-shutdown wiring:
//
//	(*api.Server, SchedulerLifecycle, func(), error)
//
// A real api.Server is not constructed; the test only verifies the type
// contract using a stub function.
func TestNewAPIServerDependencyTypeContractIsCorrect(t *testing.T) {
	t.Parallel()

	called := false
	deps := Dependencies{
		NewAPIServer: func(_ context.Context, _ config.Config, _ *slog.Logger) (*api.Server, SchedulerLifecycle, func(), error) {
			called = true
			// Return nil server to avoid needing real repositories.  The test
			// only checks that the function signature compiles and is invoked.
			return nil, nil, func() {}, nil
		},
	}

	server, sched, cleanup, err := deps.NewAPIServer(context.Background(), config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewAPIServer returned unexpected error: %v", err)
	}
	// server is nil in this stub — that is expected.
	_ = server
	// sched may legitimately be nil when no scheduler is configured.
	_ = sched
	if cleanup == nil {
		t.Fatal("cleanup is nil")
	}
	if !called {
		t.Fatal("NewAPIServer was not called")
	}
	cleanup()
}
