package jobs

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrNotFound = errors.New("job not found")

type JobRecord struct {
	ID        string    `json:"id"`
	RequestID string    `json:"request_id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func RecordFromJob(job Job) JobRecord {
	return JobRecord{ID: job.ID, RequestID: job.RequestID, Name: job.Name, Status: job.Status, CreatedAt: job.CreatedAt}
}

func (record JobRecord) Job() Job {
	return Job{ID: record.ID, RequestID: record.RequestID, Name: record.Name, Status: record.Status, CreatedAt: record.CreatedAt}
}

type Store interface {
	FindByRequestID(context.Context, string) (JobRecord, error)
	Save(context.Context, JobRecord) error
	FindByID(context.Context, string) (JobRecord, error)
}

type MemoryStore struct {
	mu        sync.Mutex
	byID      map[string]JobRecord
	byRequest map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: make(map[string]JobRecord), byRequest: make(map[string]string)}
}

func (s *MemoryStore) FindByRequestID(ctx context.Context, requestID string) (JobRecord, error) {
	if err := ctx.Err(); err != nil {
		return JobRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byRequest[requestID]
	if !ok {
		return JobRecord{}, ErrNotFound
	}
	return s.byID[id], nil
}

func (s *MemoryStore) Save(ctx context.Context, record JobRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[record.ID] = record
	s.byRequest[record.RequestID] = record.ID
	return nil
}

func (s *MemoryStore) FindByID(ctx context.Context, id string) (JobRecord, error) {
	if err := ctx.Err(); err != nil {
		return JobRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.byID[id]
	if !ok {
		return JobRecord{}, ErrNotFound
	}
	return record, nil
}
