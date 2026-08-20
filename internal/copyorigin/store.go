package copyorigin

import (
	"context"

	"github.com/google/uuid"
)

type Store interface {
	RegisterRun(context.Context, *Run) (*Run, error)
	GetRun(context.Context, uuid.UUID) (*Run, error)
}
