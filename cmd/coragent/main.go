// Package main is the first-party Coragent terminal frontend.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/blkcor/coragent/pkg/agent"
	"github.com/blkcor/coragent/tui"
)

const finalCloseLimit = 2 * time.Second

func main() {
	if err := run(); err != nil {
		// Bubble Tea has returned before this write, so terminal modes and the
		// alternate screen have already been restored.
		_, _ = fmt.Fprintln(os.Stderr, tui.SanitizeString(err.Error()))
		os.Exit(1)
	}
}

func run() error {
	settings, err := agent.LoadSettings()
	if err != nil {
		return fmt.Errorf("coragent: startup configuration failed: %w", err)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("coragent: determine working directory: %w", err)
	}
	session, err := agent.Bootstrap(settings, agent.BootstrapOptions{WorkingDirectory: workingDirectory})
	if err != nil {
		return fmt.Errorf("coragent: session startup failed: %w", err)
	}

	port := tui.NewAgentSessionAdapter(session)
	if _, err := port.Describe(context.Background()); err != nil {
		_ = closeSession(session)
		return fmt.Errorf("coragent: session description failed")
	}

	model := tui.NewAppModel(port, tui.WithVisualMode(visualModeFromEnvironment(os.LookupEnv)))
	final, programErr := tea.NewProgram(model).Run()
	postRunCloseErr := closeSession(session)

	status := model.Status()
	if returned, ok := final.(*tui.AppModel); ok {
		status = returned.Status()
	} else if final != nil {
		programErr = errors.Join(programErr, errors.New("coragent: terminal returned an unexpected model"))
	}

	var failures []error
	if programErr != nil {
		failures = append(failures, fmt.Errorf("coragent: terminal session failed: %w", programErr))
	}
	if status.Startup != nil {
		failures = append(failures, errors.New("coragent: frontend startup failed"))
	}
	if status.Fatal != nil {
		failures = append(failures, errors.New("coragent: observed-event protocol failed"))
	}
	if status.Forced {
		failures = append(failures, errors.New("coragent: forced shutdown after cancellation timeout"))
	}
	if status.Close != nil || postRunCloseErr != nil {
		failures = append(failures, errors.New("coragent: session close failed"))
	}
	return errors.Join(failures...)
}

func closeSession(session *agent.Session) error {
	if session == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), finalCloseLimit)
	defer cancel()
	return session.Close(ctx)
}

func visualModeFromEnvironment(lookup func(string) (string, bool)) tui.VisualMode {
	value := func(name string) string {
		resolved, _ := lookup(name)
		return resolved
	}
	return tui.ResolveVisualMode(tui.VisualOptions{
		Color:         tui.ColorTrueColor,
		NoColor:       value("NO_COLOR"),
		Term:          value("TERM"),
		ASCII:         enabledEnvironmentFlag(value("CORAGENT_ASCII")),
		ReducedMotion: enabledEnvironmentFlag(value("CORAGENT_REDUCED_MOTION")),
	})
}

func enabledEnvironmentFlag(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}
