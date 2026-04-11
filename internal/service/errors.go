package service

import (
	"errors"

	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// ServiceError is returned by service methods for caller-visible failures.
// The Status field maps directly to an HTTP status code.
type ServiceError struct {
	Status  int
	Message string
}

func (e *ServiceError) Error() string { return e.Message }

func isNotFound(err error) bool {
	return errors.Is(err, repository.ErrNotFound)
}
