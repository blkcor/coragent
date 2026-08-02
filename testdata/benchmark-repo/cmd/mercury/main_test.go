package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"mercury/internal/jobs"
)

type fixedIDs struct{}

func (fixedIDs) NewID() string { return "job-test" }

func TestSubmitCommandTrace(t *testing.T) {
	service := jobs.NewService(jobs.NewMemoryStore(), fixedIDs{}, func() time.Time { return time.Unix(1, 0) })
	var out bytes.Buffer
	if err := run(context.Background(), []string{"submit", "--request-id", "request-1", "--name", "report"}, service, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "submitted job-test\n" {
		t.Fatalf("output = %q", out.String())
	}
	if err := run(context.Background(), []string{"submit", "--request-id", "request-1", "--name", "report"}, service, &out); !errors.Is(err, jobs.ErrDuplicateRequest) {
		t.Fatalf("duplicate = %v", err)
	}
}

type inspectService struct{ job jobs.Job }

func (s inspectService) Submit(context.Context, jobs.SubmitRequest) (jobs.Job, error) {
	return jobs.Job{}, errors.New("not used")
}
func (s inspectService) Inspect(context.Context, string) (jobs.Job, error) { return s.job, nil }

func TestInspectTextOutput(t *testing.T) {
	var out bytes.Buffer
	service := inspectService{job: jobs.Job{ID: "job-1", Status: "queued", Name: "report"}}
	if err := run(context.Background(), []string{"inspect", "--id", "job-1"}, service, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "id=job-1 status=queued name=report\n" {
		t.Fatalf("output = %q", out.String())
	}
}
