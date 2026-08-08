//go:build darwin

package macos

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/blkcor/coragent/internal/sandbox"
	"github.com/blkcor/coragent/internal/sandbox/nop"
)

var _ sandbox.Sandbox = (*Sandbox)(nil)

// Sandbox implements sandbox.Sandbox using macOS sandbox-exec with a Seatbelt
// profile. It wraps the NOP sandbox for process-group management and I/O control,
// and adds kernel-level filesystem and network confinement.
type Sandbox struct {
	nop *nop.Sandbox
}

// New creates a macOS Seatbelt-backed sandbox. pty may be nil to disable PTY
// allocation.
func New(pty sandbox.PTYManager) *Sandbox {
	return &Sandbox{nop: nop.New(pty)}
}

// ConfinementLevel returns ConfinementKernel.
func (s *Sandbox) ConfinementLevel() sandbox.ConfinementLevel {
	return sandbox.ConfinementKernel
}

// isAvailable checks whether sandbox-exec is on PATH. It is the caller's
// responsibility to decide whether to fall back to a weaker sandbox when
// unavailable.
func isAvailable() bool {
	_, err := exec.LookPath("sandbox-exec")
	return err == nil
}

// Start launches a command inside a Seatbelt sandbox. It generates a profile
// from the spec, writes it to a temporary file, and executes the command via
// sandbox-exec. The wrapped command inherits process-group management and I/O
// control from the NOP sandbox.
func (s *Sandbox) Start(ctx context.Context, spec sandbox.CommandSpec) (sandbox.Process, error) {
	if !isAvailable() {
		return nil, fmt.Errorf("macos sandbox: sandbox-exec not found on PATH")
	}

	profile := GenerateProfile(spec)

	tmpFile, err := os.CreateTemp("", "coragent-seatbelt-*.sb")
	if err != nil {
		return nil, fmt.Errorf("macos sandbox: create profile temp file: %w", err)
	}
	if _, err := tmpFile.WriteString(profile); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		return nil, fmt.Errorf("macos sandbox: write profile: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFile.Name())
		return nil, fmt.Errorf("macos sandbox: close profile: %w", err)
	}

	wrappedSpec := spec
	wrappedSpec.Command = "/usr/bin/sandbox-exec"
	wrappedSpec.Args = append([]string{"-f", tmpFile.Name(), spec.Command}, spec.Args...)

	proc, err := s.nop.Start(ctx, wrappedSpec)
	if err != nil {
		_ = os.Remove(tmpFile.Name())
		return nil, err
	}

	// Remove the profile file once the process exits. The profile is only
	// read once at sandbox-exec startup.
	go func() {
		<-proc.Done()
		_ = os.Remove(tmpFile.Name())
	}()

	return proc, nil
}
