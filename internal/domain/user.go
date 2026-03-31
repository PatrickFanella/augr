package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// User represents an application user used for authentication.
type User struct {
	ID           uuid.UUID `json:"id"`
	Username     string    `json:"username"`
	Password     string    `json:"-"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ValidateForCreate checks that the user has the required fields for registration.
func (u *User) ValidateForCreate() error {
	if u == nil {
		return fmt.Errorf("user is required")
	}
	if strings.TrimSpace(u.Username) == "" {
		return fmt.Errorf("username is required")
	}
	if u.Password == "" {
		return fmt.Errorf("password is required")
	}
	return nil
}
