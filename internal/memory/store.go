package memory

import (
	"context"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

type AgentMemory = domain.AgentMemory
type AgentRole = domain.AgentRole

// MemoryStore defines persistence and retrieval operations for agent memory.
type MemoryStore interface {
	Store(ctx context.Context, memory AgentMemory) error
	Search(ctx context.Context, role AgentRole, query string, limit int) ([]AgentMemory, error)
	GetByRun(ctx context.Context, runID uuid.UUID) ([]AgentMemory, error)
	Delete(ctx context.Context, id uuid.UUID) error
}
