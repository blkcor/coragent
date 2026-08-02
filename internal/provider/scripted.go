package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// Turn scripts one provider call for the offline fake.
type Turn struct {
	// Text is the assistant text returned on success.
	Text string
	// Deltas, when non-empty, are emitted in order by Stream. Text is still the
	// completed block; when empty it is the concatenation of Deltas.
	Deltas []string
	// ToolCalls scripts complete tool calls (consumed by tests from S1.5).
	ToolCalls []ToolCall
	// Fail, when non-nil, makes the call return this classified failure.
	Fail *Failure
	// FailAfterDeltas returns a classified failure after streaming Deltas. It
	// models a transport that breaks after yielding a partial response.
	FailAfterDeltas *Failure
	// BlockUntilCancel makes the call wait until the request context is
	// cancelled before producing the outcome above. With neither Text nor
	// Fail set, the call returns the context error. This proves cancellation
	// propagates through the provider boundary.
	BlockUntilCancel bool
	Usage            Usage
	Reason           TerminalReason
}

// Scripted is the offline fake Provider. It consumes one Turn per call, in
// order, records every request, and never contacts a real model.
type Scripted struct {
	mu         sync.Mutex
	turns      []Turn
	requests   []Request
	cancelSeen int
}

// NewScripted returns a fake provider that plays turns in order.
func NewScripted(turns ...Turn) *Scripted {
	return &Scripted{turns: append([]Turn(nil), turns...)}
}

func (s *Scripted) Identity() Identity {
	digest := sha256.Sum256([]byte("scripted-offline-provider"))
	credentialDigest := sha256.Sum256([]byte("scripted-offline-credential"))
	return Identity{
		Adapter: "scripted-offline", WireProtocol: "scripted-v1",
		EndpointSHA256: hex.EncodeToString(digest[:]), CredentialSourceSHA256: hex.EncodeToString(credentialDigest[:]),
		Model: "scripted-offline", ToolChoice: "auto",
	}
}

// Complete implements Provider.
func (s *Scripted) Complete(ctx context.Context, req Request) (Response, error) {
	return s.play(ctx, req, nil)
}

// Stream implements StreamProvider for multi-chunk and redaction tests.
func (s *Scripted) Stream(ctx context.Context, req Request, onText func(string) error) (Response, error) {
	return s.play(ctx, req, onText)
}

func (s *Scripted) play(ctx context.Context, req Request, onText func(string) error) (Response, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	if len(s.turns) == 0 {
		s.mu.Unlock()
		return Response{}, &Failure{Class: ClassProtocol, Message: "scripted provider: no turn scripted"}
	}
	turn := s.turns[0]
	s.turns = s.turns[1:]
	s.mu.Unlock()

	if turn.BlockUntilCancel {
		<-ctx.Done()
		s.mu.Lock()
		s.cancelSeen++
		s.mu.Unlock()
		if turn.Fail == nil && turn.Text == "" {
			return Response{}, ctx.Err()
		}
	}
	if turn.Fail != nil {
		return Response{}, turn.Fail
	}
	text := turn.Text
	if len(turn.Deltas) > 0 {
		text = ""
		for _, delta := range turn.Deltas {
			if err := ctx.Err(); err != nil {
				return Response{}, err
			}
			text += delta
			if onText != nil {
				if err := onText(delta); err != nil {
					return Response{}, err
				}
			}
		}
		if turn.FailAfterDeltas != nil {
			return Response{}, turn.FailAfterDeltas
		}
	} else if onText != nil && text != "" {
		if err := onText(text); err != nil {
			return Response{}, err
		}
	}
	return Response{Text: text, ToolCalls: turn.ToolCalls, Usage: turn.Usage, Reason: turn.Reason}, nil
}

// Requests returns every request received, in call order.
func (s *Scripted) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

// CancelObserved reports how many blocking calls observed context
// cancellation.
func (s *Scripted) CancelObserved() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelSeen
}
