package makerquote

import (
	"context"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/predictionreplay"
)

type Store interface {
	RegisterCandidate(context.Context, *Candidate) (*Candidate, error)
	GetCandidate(context.Context, uuid.UUID, *predictionreplay.Recorder) (*Candidate, error)
}
