# P0 Ollama Prompt Capacity Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Eliminate Ollama HTTP 413 failures by compacting debate context below a conservative byte ceiling before any provider call.

**Architecture:** Keep provider-neutral request validation in `internal/llm`, and put domain-aware context compaction in the debate package. Preserve the system prompt, every analyst role heading, and the newest debate rounds; clip report bodies and discard oldest rounds first. Log compaction statistics and fail locally before HTTP when the system prompt alone cannot fit.

**Tech Stack:** Go, slog, existing `llm.Provider`, table-driven tests.

---

## File map

- Create `internal/llm/prompt_size.go`: byte accounting and typed oversize error.
- Create `internal/llm/prompt_size_test.go`: exact boundary and UTF-8 tests.
- Create `internal/agent/debate/prompt_budget.go`: debate-aware deterministic compaction.
- Create `internal/agent/debate/prompt_budget_test.go`: preservation and compaction tests.
- Modify `internal/agent/debate/base.go`: invoke preflight, log statistics, and never call the provider on irreducible overflow.
- Modify `internal/agent/debate/base_test.go`: integration tests at the provider boundary.

### Task 1: Add provider-neutral prompt byte validation

**Files:**
- Create: `internal/llm/prompt_size.go`
- Create: `internal/llm/prompt_size_test.go`

- [ ] **Step 1: Write failing boundary tests**

```go
func TestValidatePromptBytes(t *testing.T) {
	tests := []struct {
		name    string
		messages []llm.Message
		limit   int
		wantErr bool
	}{
		{name: "at limit", messages: []llm.Message{{Role: "user", Content: "1234"}}, limit: 4},
		{name: "over limit", messages: []llm.Message{{Role: "user", Content: "12345"}}, limit: 4, wantErr: true},
		{name: "utf8 counts bytes", messages: []llm.Message{{Role: "user", Content: "é"}}, limit: 1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := llm.ValidatePromptBytes(tt.messages, tt.limit)
			if (err != nil) != tt.wantErr { t.Fatalf("error = %v, wantErr %v", err, tt.wantErr) }
		})
	}
}
```

- [ ] **Step 2: Run the tests and verify they fail**

Run: `go test ./internal/llm -run 'TestValidatePromptBytes' -count=1`

Expected: compile failure because `ValidatePromptBytes` does not exist.

- [ ] **Step 3: Implement byte accounting and a typed error**

```go
package llm

import "fmt"

type PromptTooLargeError struct { ActualBytes, MaxBytes int }

func (e *PromptTooLargeError) Error() string {
	return fmt.Sprintf("llm: prompt is %d bytes; maximum is %d", e.ActualBytes, e.MaxBytes)
}

func PromptBytes(messages []Message) int {
	total := 0
	for _, message := range messages { total += len([]byte(message.Content)) }
	return total
}

func ValidatePromptBytes(messages []Message, maxBytes int) error {
	actual := PromptBytes(messages)
	if maxBytes > 0 && actual > maxBytes {
		return &PromptTooLargeError{ActualBytes: actual, MaxBytes: maxBytes}
	}
	return nil
}
```

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/llm -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/llm/prompt_size.go internal/llm/prompt_size_test.go
git commit -m "feat(llm): add prompt byte validation"
```

### Task 2: Compact debate context deterministically

**Files:**
- Create: `internal/agent/debate/prompt_budget.go`
- Create: `internal/agent/debate/prompt_budget_test.go`

- [ ] **Step 1: Write failing tests for preservation and ordering**

Add tests that construct 20 rounds with 8 KiB contributions and three 20 KiB reports, call `buildBudgetedDebateMessages`, and assert:

```go
if got := llm.PromptBytes(messages); got > maxDebatePromptBytes {
	t.Fatalf("prompt bytes = %d, max = %d", got, maxDebatePromptBytes)
}
if messages[0].Content != systemPrompt { t.Fatal("system prompt changed") }
if !strings.Contains(messages[1].Content, "Round 20:") { t.Fatal("newest round missing") }
if strings.Contains(messages[1].Content, "Round 1:") { t.Fatal("oldest round was not discarded") }
for _, role := range []agent.AgentRole{agent.AgentRoleMarketAnalyst, agent.AgentRoleNewsAnalyst, agent.AgentRoleFundamentalsAnalyst} {
	if !strings.Contains(messages[1].Content, string(role)+":") { t.Fatalf("role heading %s missing", role) }
}
```

Add a second test asserting an oversized system prompt returns `*llm.PromptTooLargeError` and no messages.

- [ ] **Step 2: Run the tests and verify they fail**

Run: `go test ./internal/agent/debate -run 'TestBuildBudgetedDebateMessages' -count=1`

Expected: compile failure because the helper does not exist.

- [ ] **Step 3: Implement the compactor**

Use these exact contracts and limits:

```go
const (
	maxDebatePromptBytes = 60 * 1024
	maxAnalystReportBytes = 8 * 1024
)

type promptCompactionStats struct {
	OriginalBytes int
	FinalBytes int
	DroppedRounds int
	TruncatedReports int
}

func buildBudgetedDebateMessages(systemPrompt string, rounds []agent.DebateRound, reports map[agent.AgentRole]string) ([]llm.Message, promptCompactionStats, error)
func truncateUTF8(value string, maxBytes int) (string, bool)
```

Implementation rules:
1. Reject the unchanged system prompt if it leaves fewer than 1,024 bytes for the user message.
2. Sort report roles exactly as `formatAnalystReportsForPrompt` does; retain every role heading and truncate each body to 8 KiB on a UTF-8 boundary, appending `\n[truncated]`.
3. Add rounds from newest to oldest while the final two-message request remains at or below 60 KiB; reverse selected rounds before formatting so chronology remains ascending.
4. If fixed labels plus clipped reports still exceed the budget, divide the available report-body bytes evenly across sorted roles and truncate again.
5. Call `llm.ValidatePromptBytes` on the final messages before returning.

- [ ] **Step 4: Run the debate tests**

Run: `go test ./internal/agent/debate -count=1`

Expected: PASS, including existing exact prompt formatting tests for small inputs.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/debate/prompt_budget.go internal/agent/debate/prompt_budget_test.go
git commit -m "feat(agent): compact oversized debate prompts"
```

### Task 3: Enforce the budget before provider calls

**Files:**
- Modify: `internal/agent/debate/base.go:54-86`
- Modify: `internal/agent/debate/base_test.go`

- [ ] **Step 1: Add failing provider-boundary tests**

Add `TestBaseDebaterCallWithContextCompactsBeforeProvider` and `TestBaseDebaterCallWithContextRejectsOversizedSystemPrompt`. Assert the first sends at most 60 KiB and calls the provider once; assert the second returns a typed size error and `mock.calls.Load() == 0`.

- [ ] **Step 2: Run the tests and verify failure**

Run: `go test ./internal/agent/debate -run 'TestBaseDebaterCallWithContext(Compacts|Rejects)' -count=1`

Expected: FAIL because `CallWithContext` still sends the unbounded request.

- [ ] **Step 3: Replace direct message construction with the budget helper**

```go
messages, stats, err := buildBudgetedDebateMessages(systemPrompt, previousRounds, analystReports)
if err != nil {
	return "", "", llm.CompletionUsage{}, fmt.Errorf("%s: prompt preflight failed: %w", errorPrefix, err)
}
if stats.OriginalBytes != stats.FinalBytes {
	b.logger.WarnContext(ctx, "debate prompt compacted",
		"role", b.role, "phase", b.phase,
		"original_bytes", stats.OriginalBytes, "final_bytes", stats.FinalBytes,
		"dropped_rounds", stats.DroppedRounds, "truncated_reports", stats.TruncatedReports)
}
promptText := agent.PromptTextFromMessages(messages)
```

- [ ] **Step 4: Run targeted and full tests**

Run: `go test ./internal/agent/debate ./internal/llm -count=1`

Run: `go test ./internal/... -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/debate/base.go internal/agent/debate/base_test.go
git commit -m "fix(agent): preflight debate prompt capacity"
```

### Task 4: Production canary and acceptance

- [ ] Deploy the app image without changing model configuration.
- [ ] Trigger one stock strategy known to have failed with HTTP 413.
- [ ] Verify logs contain `debate prompt compacted` with `final_bytes <= 61440` and no Ollama HTTP 413.
- [ ] Capture the provider-boundary serialized request size for the canary and verify it remains below 65,536 bytes; the 60 KiB content budget is intentional JSON/role overhead headroom.
- [ ] Observe at least 20 scheduled stock runs.
- [ ] Accept P0 only when prompt-size 413 count is zero and no preflight failure occurs for valid system prompts.
- [ ] Roll back the app image if compaction causes malformed required JSON in research-manager output; retained prompt text in the decision journal remains the debugging artifact.
