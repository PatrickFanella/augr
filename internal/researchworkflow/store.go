package researchworkflow

import (
	"context"

	"github.com/google/uuid"
)

type Store interface {
	RegisterWorkflow(context.Context, *Hypothesis, *Critic, Parents) (*Hypothesis, *Critic, error)
	GetWorkflow(context.Context, uuid.UUID, Parents) (*Hypothesis, *Critic, error)
}
