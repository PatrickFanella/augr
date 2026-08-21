package generativestrategy

import (
	"context"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type Store interface {
	RegisterCompilation(context.Context, *Spec, *strategycatalog.Version, *Receipt) (*Spec, *strategycatalog.Version, *Receipt, error)
	GetCompilation(context.Context, uuid.UUID) (*Spec, *strategycatalog.Version, *Receipt, error)
}
