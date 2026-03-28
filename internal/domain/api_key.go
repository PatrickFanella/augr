package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// APIKey represents a hashed API key stored for programmatic access.
type APIKey struct {
	ID                 uuid.UUID  `json:"id"`
	Name               string     `json:"name"`
	KeyPrefix          string     `json:"key_prefix"`
	KeyHash            string     `json:"-"`
	RateLimitPerMinute int        `json:"rate_limit_per_minute"`
	LastUsedAt         *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	RevokedAt          *time.Time `json:"revoked_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// Validate checks that the API key metadata is well-formed before persistence.
func (k *APIKey) Validate() error {
	if strings.TrimSpace(k.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(k.KeyPrefix) == "" {
		return fmt.Errorf("key prefix is required")
	}
	if strings.TrimSpace(k.KeyHash) == "" {
		return fmt.Errorf("key hash is required")
	}
	if k.RateLimitPerMinute <= 0 {
		return fmt.Errorf("rate_limit_per_minute must be greater than 0")
	}
	return nil
}
