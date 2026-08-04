package jobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fixedIDs struct{ value string }

func (f fixedIDs) NewID() string { return f.value }

func TestSubmitCreatesIDAndPersistsStatus(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(store, fixedIDs{value: "job-42"}, func() time.Time { return time.Unix(1, 0).UTC() })
	job, err := service.Submit(context.Background(), SubmitRequest{RequestID: "request-1", Name: "report"})
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "job-42" || job.Status != "queued" {
		t.Fatalf("job = %+v", job)
	}
	record, err := store.FindByID(context.Background(), "job-42")
	if err != nil || record.Status != "queued" {
		t.Fatalf("record = %+v, %v", record, err)
	}
}

func TestSubmitRejectsDuplicateRequest(t *testing.T) {
	service := NewService(NewMemoryStore(), fixedIDs{value: "job-42"}, time.Now)
	request := SubmitRequest{RequestID: "request-1", Name: "report"}
	if _, err := service.Submit(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Submit(context.Background(), request); !errors.Is(err, ErrDuplicateRequest) {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestValidateSubmit(t *testing.T) {
	if err := ValidateSubmit(SubmitRequest{}); err == nil {
		t.Fatal("empty request validates")
	}
}
