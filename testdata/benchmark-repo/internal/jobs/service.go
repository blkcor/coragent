package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrDuplicateRequest = errors.New("duplicate request ID")

type IDGenerator interface {
	NewID() string
}

type Service struct {
	store Store
	ids   IDGenerator
	now   func() time.Time
}

func NewService(store Store, ids IDGenerator, now func() time.Time) *Service {
	return &Service{store: store, ids: ids, now: now}
}

func (s *Service) Submit(ctx context.Context, request SubmitRequest) (Job, error) {
	if err := ValidateSubmit(request); err != nil {
		return Job{}, fmt.Errorf("validate submit request: %w", err)
	}
	if _, err := s.store.FindByRequestID(ctx, request.RequestID); err == nil {
		return Job{}, fmt.Errorf("%w: %s", ErrDuplicateRequest, request.RequestID)
	} else if !errors.Is(err, ErrNotFound) {
		return Job{}, fmt.Errorf("check duplicate request: %w", err)
	}
	job := Job{ID: s.ids.NewID(), RequestID: request.RequestID, Name: request.Name, Status: "queued", CreatedAt: s.now()}
	if err := s.store.Save(ctx, RecordFromJob(job)); err != nil {
		return Job{}, fmt.Errorf("save job: %w", err)
	}
	return job, nil
}

func (s *Service) Inspect(ctx context.Context, id string) (Job, error) {
	record, err := s.store.FindByID(ctx, id)
	if err != nil {
		return Job{}, err
	}
	return record.Job(), nil
}
