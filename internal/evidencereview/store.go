package evidencereview

import (
	"context"
	"github.com/google/uuid"
)

type Store interface {
	RegisterBundle(context.Context, *Case, []*Review, *Summary, CaseInput) (*Case, []*Review, *Summary, error)
	GetBundle(context.Context, uuid.UUID, uuid.UUID, CaseInput) (*Case, []*Review, *Summary, error)
}
