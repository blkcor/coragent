package worker

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

const defaultCommandTimeout = 30 * time.Second

type process interface {
	Start() error
	Wait() error
	KillProcess() error
	KillGroup() error
}

type factory interface {
	Command(string, ...string) process
}

type commandFactory struct{}

func (commandFactory) Command(name string, args ...string) process {
	return newCommandProcess(exec.Command(name, args...))
}

type Runner struct {
	factory factory
	timeout time.Duration
}

func NewRunner() *Runner {
	return &Runner{factory: commandFactory{}, timeout: defaultCommandTimeout}
}

func (r *Runner) Run(ctx context.Context, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	proc := r.factory.Command(name, args...)
	if err := proc.Start(); err != nil {
		return fmt.Errorf("start worker command: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- proc.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		killErr := proc.KillGroup()
		waitErr := <-done
		if killErr != nil {
			return fmt.Errorf("kill worker process group: %w", killErr)
		}
		if waitErr != nil && !errors.Is(waitErr, context.Canceled) {
			return ctx.Err()
		}
		return ctx.Err()
	}
}
