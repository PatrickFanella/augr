package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// RunService encapsulates operations on pipeline runs.
type RunService struct {
	runs repository.PipelineRunRepository
}

func NewRunService(runs repository.PipelineRunRepository) *RunService {
	return &RunService{runs: runs}
}

// FindByID scans pages to locate a pipeline run by ID. The repository's Get
// method requires a tradeDate which is not available from the URL.
func (svc *RunService) FindByID(ctx context.Context, id uuid.UUID) (*domain.PipelineRun, error) {
	const pageSize = 100
	offset := 0
	for {
		runs, err := svc.runs.List(ctx, repository.PipelineRunFilter{}, pageSize, offset)
		if err != nil {
			return nil, err
		}
		if len(runs) == 0 {
			return nil, nil
		}
		for i := range runs {
			if runs[i].ID == id {
				return &runs[i], nil
			}
		}
		if len(runs) < pageSize {
			return nil, nil
		}
		offset += pageSize
	}
}

// Cancel validates the state machine transition and cancels the run.
func (svc *RunService) Cancel(ctx context.Context, id uuid.UUID) error {
	run, err := svc.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if run == nil {
		return repository.ErrNotFound
	}
	if !run.Status.CanTransitionTo(domain.PipelineStatusCancelled) {
		return &ServiceError{Status: 400, Message: "run cannot be cancelled in its current state"}
	}
	update := repository.PipelineRunStatusUpdate{
		Status: domain.PipelineStatusCancelled,
	}
	return svc.runs.UpdateStatus(ctx, id, run.TradeDate, update)
}
