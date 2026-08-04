package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"mercury/internal/jobs"
)

type counterIDs struct{ next atomic.Uint64 }

func (c *counterIDs) NewID() string { return "job-" + strconv.FormatUint(c.next.Add(1), 10) }

func main() {
	service := jobs.NewService(jobs.NewMemoryStore(), &counterIDs{}, time.Now)
	if err := run(context.Background(), os.Args[1:], service, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type jobService interface {
	Submit(context.Context, jobs.SubmitRequest) (jobs.Job, error)
	Inspect(context.Context, string) (jobs.Job, error)
}

func run(ctx context.Context, args []string, service jobService, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: mercury <submit|inspect>")
	}
	switch args[0] {
	case "submit":
		return runSubmit(ctx, args[1:], service, out)
	case "inspect":
		return runInspect(ctx, args[1:], service, out)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runSubmit(ctx context.Context, args []string, service jobService, out io.Writer) error {
	flags := flag.NewFlagSet("submit", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	requestID := flags.String("request-id", "", "idempotency request ID")
	name := flags.String("name", "", "job name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	request := jobs.SubmitRequest{RequestID: *requestID, Name: *name}
	if err := jobs.ValidateSubmit(request); err != nil {
		return fmt.Errorf("submit arguments: %w", err)
	}
	job, err := service.Submit(ctx, request)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "submitted %s\n", job.ID)
	return nil
}

func runInspect(ctx context.Context, args []string, service jobService, out io.Writer) error {
	flags := flag.NewFlagSet("inspect", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	id := flags.String("id", "", "job ID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	job, err := service.Inspect(ctx, *id)
	if err != nil {
		return err
	}
	// Existing text output is the compatibility baseline for E03.
	fmt.Fprintf(out, "id=%s status=%s name=%s\n", job.ID, job.Status, job.Name)
	return nil
}
