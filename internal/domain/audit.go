package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AuditLogEntry represents a single auditable system event.
type AuditLogEntry struct {
	ID         uuid.UUID       `json:"id"`
	EventType  string          `json:"event_type"`
	EntityType string          `json:"entity_type,omitempty"`
	EntityID   *uuid.UUID      `json:"entity_id,omitempty"`
	Actor      string          `json:"actor,omitempty"`
	Details    json.RawMessage `json:"details,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}
