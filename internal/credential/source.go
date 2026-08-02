// Package credential owns runtime Provider credentials. Values obtained from a
// Source may be passed only to a Provider transport credential field; callers
// must never place them in prompts, tools, events, logs, or durable state.
package credential

import (
	"context"
	"errors"
	"fmt"
	"os"
)

var ErrUnavailable = errors.New("credential: provider credential unavailable")

// Source returns a runtime credential without exposing where it is stored.
type Source interface {
	Credential(context.Context) (string, error)
	SourceIdentity() string
}

// EnvSource reads one explicitly named environment variable. The name is
// configuration; the returned value is runtime-only.
type EnvSource struct {
	Name string
}

func (s EnvSource) SourceIdentity() string { return "environment:" + s.Name }

func (s EnvSource) Credential(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("credential: load: %w", err)
	}
	if s.Name == "" {
		return "", fmt.Errorf("%w: environment variable name is empty", ErrUnavailable)
	}
	value, ok := os.LookupEnv(s.Name)
	if !ok || value == "" {
		return "", fmt.Errorf("%w: configured environment variable is not set", ErrUnavailable)
	}
	return value, nil
}

// Static is intended for offline tests. Production configuration should use a
// source that obtains the credential only when the transport request begins.
type Static struct {
	Value string
}

func (s Static) SourceIdentity() string { return "static-test-source" }

func (s Static) Credential(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.Value == "" {
		return "", ErrUnavailable
	}
	return s.Value, nil
}
