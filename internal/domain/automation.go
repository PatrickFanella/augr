package domain

import "time"

// AutomationJobControl stores the durable operator enable/disable override for
// a registered automation job.
type AutomationJobControl struct {
	JobName   string    `json:"job_name"`
	Enabled   bool      `json:"enabled"`
	UpdatedBy string    `json:"updated_by"`
	UpdatedAt time.Time `json:"updated_at"`
}
