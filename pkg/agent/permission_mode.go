package agent

import (
	"errors"
	"fmt"

	"github.com/blkcor/coragent/internal/permission"
)

// PermissionMode is the public, typed permission posture used by the standard
// permission engine.
type PermissionMode string

const (
	PermissionModeDefault         PermissionMode = "default"
	PermissionModeAutoAcceptEdits PermissionMode = "auto-accept-edits"
	PermissionModePlan            PermissionMode = "plan"
	PermissionModeBypass          PermissionMode = "bypass"
)

// ErrPermissionModeChangeInFlight is retained for source compatibility. Standard
// sessions now support linearized live mode changes and no longer return it.
// Deprecated: mode changes are allowed while a run is in flight.
var ErrPermissionModeChangeInFlight = errors.New("agent: permission mode can change only between runs")

// ErrPermissionModeExternallyOwned reports that a caller-supplied Dispatcher,
// not Coragent's standard engine, owns permission behavior.
var ErrPermissionModeExternallyOwned = errors.New("agent: permission mode is controlled by the caller-supplied Dispatcher")

// PermissionMode returns the current typed posture for a session using the
// standard engine.
func (s *Session) PermissionMode() (PermissionMode, error) {
	if s.permission == nil {
		return "", ErrPermissionModeExternallyOwned
	}
	return publicPermissionMode(s.permission.Mode()), nil
}

// SetPermissionModeTyped changes the standard permission posture. The setter is
// linearized by the permission engine: decisions that begin after it returns see
// the new mode, while a permission request already open keeps its snapshotted
// mode and still requires an explicit reply.
func (s *Session) SetPermissionModeTyped(mode PermissionMode) error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	if s.permission == nil {
		return ErrPermissionModeExternallyOwned
	}
	internal, err := internalPermissionMode(mode)
	if err != nil {
		return err
	}
	s.permission.SetMode(internal)
	return nil
}

func internalPermissionMode(mode PermissionMode) (permission.Mode, error) {
	switch mode {
	case PermissionModeDefault:
		return permission.ModeDefault, nil
	case PermissionModeAutoAcceptEdits:
		return permission.ModeAutoAcceptEdits, nil
	case PermissionModePlan:
		return permission.ModePlan, nil
	case PermissionModeBypass:
		return permission.ModeBypass, nil
	default:
		return permission.ModeDefault, fmt.Errorf("permission: unknown mode %q", mode)
	}
}

func publicPermissionMode(mode permission.Mode) PermissionMode {
	switch mode {
	case permission.ModeAutoAcceptEdits:
		return PermissionModeAutoAcceptEdits
	case permission.ModePlan:
		return PermissionModePlan
	case permission.ModeBypass:
		return PermissionModeBypass
	default:
		return PermissionModeDefault
	}
}
