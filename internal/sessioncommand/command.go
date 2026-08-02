// Package sessioncommand defines the serializable control envelope used by
// every frontend to change session state.
//
// A SessionCommand is control-plane input. It is not a shell command and it
// never carries provider- or frontend-specific data. JSON is the durable
// encoding. New command kinds (resume, close, approval answers, steering) are
// added as new Kind values with their own payload structs; the envelope shape
// does not change.
package sessioncommand

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Kind identifies the state change a Command requests.
type Kind string

const (
	// KindSubmit submits a user prompt and starts a new run.
	KindSubmit Kind = "submit"
	// KindCancel requests cancellation of the active run.
	KindCancel Kind = "cancel"
	// KindResume records that a loaded open session is being resumed.
	KindResume Kind = "resume"
	// KindClose closes a session non-destructively.
	KindClose Kind = "close"
)

// Command is the serializable SessionCommand envelope. Every command has a
// unique ID used for correlation and duplicate rejection. Payload holds the
// kind-specific data as JSON.
type Command struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id,omitempty"`
	Kind      Kind            `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
}

// SubmitPayload carries the user prompt text for a submit command.
type SubmitPayload struct {
	Prompt string `json:"prompt"`
}

// CancelPayload carries no data in M1: cancel always targets the single
// active run. The struct exists so later fields (for example an explicit run
// target) extend the payload without changing the envelope.
type CancelPayload struct{}

type ResumePayload struct{}
type ClosePayload struct{}

// NewSubmit builds a submit command with the given ID and prompt.
func NewSubmit(id, prompt string) (Command, error) {
	payload, err := json.Marshal(SubmitPayload{Prompt: prompt})
	if err != nil {
		return Command{}, fmt.Errorf("sessioncommand: marshal submit payload: %w", err)
	}
	return Command{ID: id, Kind: KindSubmit, Payload: payload}, nil
}

// NewCancel builds a cancel command with the given ID.
func NewCancel(id string) (Command, error) {
	payload, err := json.Marshal(CancelPayload{})
	if err != nil {
		return Command{}, fmt.Errorf("sessioncommand: marshal cancel payload: %w", err)
	}
	return Command{ID: id, Kind: KindCancel, Payload: payload}, nil
}

func NewResume(id string) (Command, error) {
	payload, err := json.Marshal(ResumePayload{})
	if err != nil {
		return Command{}, fmt.Errorf("sessioncommand: marshal resume payload: %w", err)
	}
	return Command{ID: id, Kind: KindResume, Payload: payload}, nil
}

func NewClose(id string) (Command, error) {
	payload, err := json.Marshal(ClosePayload{})
	if err != nil {
		return Command{}, fmt.Errorf("sessioncommand: marshal close payload: %w", err)
	}
	return Command{ID: id, Kind: KindClose, Payload: payload}, nil
}

// ForSession correlates a command to an intended session. Empty SessionID is
// accepted for direct in-process callers; frontends should set it.
func (c Command) ForSession(sessionID string) Command {
	c.SessionID = sessionID
	return c
}

// Validate reports whether the command is well formed: a non-empty ID, a
// known kind, and a payload that decodes for that kind. Validation does not
// check duplicate IDs; the session owns idempotency.
func (c Command) Validate() error {
	if c.ID == "" {
		return errors.New("sessioncommand: command ID is required")
	}
	switch c.Kind {
	case KindSubmit:
		p, err := c.DecodeSubmit()
		if err != nil {
			return err
		}
		if p.Prompt == "" {
			return errors.New("sessioncommand: submit prompt is empty")
		}
		if len([]byte(p.Prompt)) > 256*1024 {
			return errors.New("sessioncommand: submit prompt exceeds 256 KiB")
		}
		return nil
	case KindCancel:
		_, err := c.DecodeCancel()
		return err
	case KindResume:
		_, err := c.DecodeResume()
		return err
	case KindClose:
		_, err := c.DecodeClose()
		return err
	default:
		return fmt.Errorf("sessioncommand: unknown command kind %q", c.Kind)
	}
}

func (c Command) DecodeResume() (ResumePayload, error) {
	if c.Kind != KindResume {
		return ResumePayload{}, fmt.Errorf("sessioncommand: cannot decode kind %q as %q", c.Kind, KindResume)
	}
	var p ResumePayload
	if err := c.decode(&p); err != nil {
		return ResumePayload{}, err
	}
	return p, nil
}

func (c Command) DecodeClose() (ClosePayload, error) {
	if c.Kind != KindClose {
		return ClosePayload{}, fmt.Errorf("sessioncommand: cannot decode kind %q as %q", c.Kind, KindClose)
	}
	var p ClosePayload
	if err := c.decode(&p); err != nil {
		return ClosePayload{}, err
	}
	return p, nil
}

// DecodeSubmit decodes the payload of a submit command.
func (c Command) DecodeSubmit() (SubmitPayload, error) {
	if c.Kind != KindSubmit {
		return SubmitPayload{}, fmt.Errorf("sessioncommand: cannot decode kind %q as %q", c.Kind, KindSubmit)
	}
	var p SubmitPayload
	if err := c.decode(&p); err != nil {
		return SubmitPayload{}, err
	}
	return p, nil
}

// DecodeCancel decodes the payload of a cancel command.
func (c Command) DecodeCancel() (CancelPayload, error) {
	if c.Kind != KindCancel {
		return CancelPayload{}, fmt.Errorf("sessioncommand: cannot decode kind %q as %q", c.Kind, KindCancel)
	}
	var p CancelPayload
	if err := c.decode(&p); err != nil {
		return CancelPayload{}, err
	}
	return p, nil
}

func (c Command) decode(v any) error {
	if len(c.Payload) == 0 {
		return fmt.Errorf("sessioncommand: command %q has no payload", c.ID)
	}
	dec := json.NewDecoder(strings.NewReader(string(c.Payload)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("sessioncommand: decode %s payload: %w", c.Kind, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("sessioncommand: decode %s payload: trailing data", c.Kind)
	}
	return nil
}
