package capacity

import (
	"context"

	"github.com/google/uuid"
)

type Store interface {
	RegisterContract(context.Context, *Contract) (*Contract, error)
	GetContract(context.Context, uuid.UUID) (*Contract, error)
	RecordComparison(context.Context, *Comparison) (*Comparison, error)
	GetComparison(context.Context, uuid.UUID) (*Comparison, error)
}
