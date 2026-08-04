package worker

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeProcess struct {
	processKilled, groupKilled bool
	started, done              chan struct{}
}

func (p *fakeProcess) Start() error { close(p.started); return nil }
func (p *fakeProcess) Wait() error  { <-p.done; return errors.New("killed") }
func (p *fakeProcess) KillProcess() error {
	p.processKilled = true
	close(p.done)
	return nil
}
func (p *fakeProcess) KillGroup() error {
	p.groupKilled = true
	close(p.done)
	return nil
}

type fakeFactory struct{ process *fakeProcess }

func (f fakeFactory) Command(string, ...string) process { return f.process }

func TestRunnerCancelsFullProcessGroup(t *testing.T) {
	proc := &fakeProcess{started: make(chan struct{}), done: make(chan struct{})}
	runner := &Runner{factory: fakeFactory{process: proc}, timeout: time.Minute}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx, "ignored") }()
	select {
	case <-proc.started:
	case <-time.After(time.Second):
		t.Fatal("process did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not complete")
	}
	if !proc.groupKilled || proc.processKilled {
		t.Fatalf("groupKilled=%v processKilled=%v", proc.groupKilled, proc.processKilled)
	}
}
