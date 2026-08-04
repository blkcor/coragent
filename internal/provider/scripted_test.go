package provider

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestScriptedPlaysTurnsInOrder(t *testing.T) {
	s := NewScripted(
		Turn{Text: "first"},
		Turn{Fail: &Failure{Class: ClassPermanent, Message: "bad key"}},
	)

	resp, err := s.Complete(context.Background(), Request{Prompt: "one"})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if resp.Text != "first" {
		t.Errorf("text = %q, want %q", resp.Text, "first")
	}

	_, err = s.Complete(context.Background(), Request{Prompt: "two"})
	var fail *Failure
	if !errors.As(err, &fail) {
		t.Fatalf("second call error = %v, want *Failure", err)
	}
	if fail.Class != ClassPermanent {
		t.Errorf("class = %q, want %q", fail.Class, ClassPermanent)
	}

	_, err = s.Complete(context.Background(), Request{Prompt: "three"})
	if !errors.As(err, &fail) || fail.Class != ClassProtocol {
		t.Errorf("exhausted script error = %v, want protocol Failure", err)
	}

	reqs := s.Requests()
	if len(reqs) != 3 || reqs[0].Prompt != "one" || reqs[2].Prompt != "three" {
		t.Errorf("requests = %+v", reqs)
	}
}

func TestScriptedBlockUntilCancel(t *testing.T) {
	s := NewScripted(Turn{BlockUntilCancel: true})
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := s.Complete(ctx, Request{Prompt: "wait"})
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("Complete returned %v before cancellation", err)
	case <-time.After(20 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Complete error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Complete did not return after cancellation")
	}
	if s.CancelObserved() != 1 {
		t.Errorf("CancelObserved = %d, want 1", s.CancelObserved())
	}
}
