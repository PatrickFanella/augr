package copyreplay

import (
	"context"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
)

type Store interface {
	RegisterReplay(context.Context, *Replay) (*Replay, error)
	GetReplay(context.Context, uuid.UUID, *dataset.Manifest) (*Replay, error)
}
