package memory

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type mockInjectorMemoryRepo struct {
	results     []domain.AgentMemory
	err         error
	lastQuery   string
	lastFilter  repository.MemorySearchFilter
	lastLimit   int
	lastOffset  int
	searchCalls int
}

func (m *mockInjectorMemoryRepo) Create(_ context.Context, _ *domain.AgentMemory) error { return nil }

func (m *mockInjectorMemoryRepo) Search(_ context.Context, query string, filter repository.MemorySearchFilter, limit, offset int) ([]domain.AgentMemory, error) {
	m.searchCalls++
	m.lastQuery = query
	m.lastFilter = filter
	m.lastLimit = limit
	m.lastOffset = offset
	if m.err != nil {
		return nil, m.err
	}
	return m.results, nil
}

func (m *mockInjectorMemoryRepo) Delete(_ context.Context, _ uuid.UUID) error { return nil }

func TestInjector_GetMemoryContextAndInjectIntoMessages(t *testing.T) {
	t.Parallel()

	repo := &mockInjectorMemoryRepo{
		results: []domain.AgentMemory{
			{
				AgentRole:      domain.AgentRoleTrader,
				Situation:      "AAPL broke out after earnings",
				Recommendation: "Scale into strength",
			},
			{
				AgentRole:      domain.AgentRoleTrader,
				Situation:      "AAPL reversed after an extended run",
				Recommendation: "Tighten stops sooner",
			},
		},
	}
	injector := NewInjector(repo, discardLogger())

	memoryContext, err := injector.GetMemoryContext(context.Background(), domain.AgentRoleTrader, "AAPL", 2)
	if err != nil {
		t.Fatalf("GetMemoryContext() error = %v, want nil", err)
	}

	wantContext := "## Relevant Past Experience\n" +
		"- Situation: AAPL broke out after earnings, Lesson: Scale into strength\n" +
		"- Situation: AAPL reversed after an extended run, Lesson: Tighten stops sooner"
	if memoryContext != wantContext {
		t.Fatalf("GetMemoryContext() = %q, want %q", memoryContext, wantContext)
	}

	if repo.searchCalls != 1 {
		t.Fatalf("Search() calls = %d, want 1", repo.searchCalls)
	}
	if repo.lastQuery != "AAPL" {
		t.Errorf("Search() query = %q, want %q", repo.lastQuery, "AAPL")
	}
	if repo.lastFilter.AgentRole != domain.AgentRoleTrader {
		t.Errorf("Search() AgentRole filter = %q, want %q", repo.lastFilter.AgentRole, domain.AgentRoleTrader)
	}
	if repo.lastLimit != 2 {
		t.Errorf("Search() limit = %d, want 2", repo.lastLimit)
	}
	if repo.lastOffset != 0 {
		t.Errorf("Search() offset = %d, want 0", repo.lastOffset)
	}

	messages := []llm.Message{
		{Role: "system", Content: "Base system prompt."},
		{Role: "user", Content: "Analyze AAPL."},
	}

	got := injector.InjectIntoMessages(messages, memoryContext)

	wantMessages := []llm.Message{
		{Role: "system", Content: "Base system prompt.\n\n" + wantContext},
		{Role: "user", Content: "Analyze AAPL."},
	}
	if !reflect.DeepEqual(got, wantMessages) {
		t.Fatalf("InjectIntoMessages() = %#v, want %#v", got, wantMessages)
	}

	if messages[0].Content != "Base system prompt." {
		t.Fatalf("InjectIntoMessages() mutated original messages, got %q", messages[0].Content)
	}
}

func TestInjector_NoMemoriesLeavesMessagesUnchanged(t *testing.T) {
	t.Parallel()

	repo := &mockInjectorMemoryRepo{}
	injector := NewInjector(repo, discardLogger())

	memoryContext, err := injector.GetMemoryContext(context.Background(), domain.AgentRoleRiskManager, "MSFT", 3)
	if err != nil {
		t.Fatalf("GetMemoryContext() error = %v, want nil", err)
	}
	if memoryContext != "" {
		t.Fatalf("GetMemoryContext() = %q, want empty string", memoryContext)
	}

	messages := []llm.Message{
		{Role: "system", Content: "Risk system prompt."},
		{Role: "user", Content: "Assess MSFT risk."},
	}

	got := injector.InjectIntoMessages(messages, memoryContext)
	if !reflect.DeepEqual(got, messages) {
		t.Fatalf("InjectIntoMessages() = %#v, want unchanged %#v", got, messages)
	}
}

func TestInjector_GetMemoryContext_BlankTickerSkipsSearch(t *testing.T) {
	t.Parallel()

	repo := &mockInjectorMemoryRepo{
		results: []domain.AgentMemory{
			{
				AgentRole:      domain.AgentRoleTrader,
				Situation:      "should not be used",
				Recommendation: "should not be used",
			},
		},
	}
	injector := NewInjector(repo, discardLogger())

	memoryContext, err := injector.GetMemoryContext(context.Background(), domain.AgentRoleTrader, "   \t  ", 2)
	if err != nil {
		t.Fatalf("GetMemoryContext() error = %v, want nil", err)
	}
	if memoryContext != "" {
		t.Fatalf("GetMemoryContext() = %q, want empty string", memoryContext)
	}
	if repo.searchCalls != 0 {
		t.Fatalf("Search() calls = %d, want 0 for blank ticker", repo.searchCalls)
	}
}

func TestInjector_GetMemoryContext_NonPositiveLimitSkipsSearch(t *testing.T) {
	t.Parallel()

	repo := &mockInjectorMemoryRepo{}
	injector := NewInjector(repo, discardLogger())

	memoryContext, err := injector.GetMemoryContext(context.Background(), domain.AgentRoleTrader, "AAPL", 0)
	if err != nil {
		t.Fatalf("GetMemoryContext() error = %v, want nil", err)
	}
	if memoryContext != "" {
		t.Fatalf("GetMemoryContext() = %q, want empty string", memoryContext)
	}
	if repo.searchCalls != 0 {
		t.Fatalf("Search() calls = %d, want 0 for non-positive limit", repo.searchCalls)
	}
}

func TestInjector_GetMemoryContextReturnsSearchError(t *testing.T) {
	t.Parallel()

	repo := &mockInjectorMemoryRepo{err: errors.New("search failed")}
	injector := NewInjector(repo, discardLogger())

	_, err := injector.GetMemoryContext(context.Background(), domain.AgentRoleTrader, "TSLA", 1)
	if err == nil {
		t.Fatal("GetMemoryContext() error = nil, want search error")
	}
	if !strings.Contains(err.Error(), `role trader ticker "TSLA" limit 1`) {
		t.Fatalf("GetMemoryContext() error = %q, want wrapped context", err)
	}
}

func TestInjector_InjectIntoMessages_NoSystemMessageLeavesMessagesUnchanged(t *testing.T) {
	t.Parallel()

	injector := NewInjector(&mockInjectorMemoryRepo{}, discardLogger())
	messages := []llm.Message{
		{Role: "user", Content: "Analyze NVDA."},
		{Role: "assistant", Content: "Need more context."},
	}

	got := injector.InjectIntoMessages(messages, "## Relevant Past Experience\n- Situation: Example, Lesson: Example")
	if !reflect.DeepEqual(got, messages) {
		t.Fatalf("InjectIntoMessages() = %#v, want unchanged %#v", got, messages)
	}
}
