package memory

import (
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// MemoryStore defines persistence and retrieval operations for agent memory.
// It embeds repository.MemoryRepository so there is a single source of truth
// for the core memory persistence contract.
type MemoryStore interface {
	repository.MemoryRepository
}
