package predictionreplay

import (
	"context"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
)

type Store interface {
	RegisterRecorder(context.Context, *Recorder) (*Recorder, error)
	GetRecorder(context.Context, uuid.UUID, *dataset.Manifest) (*Recorder, error)
}
