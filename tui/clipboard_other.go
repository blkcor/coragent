//go:build !darwin

package tui

import "errors"

var errSystemClipboardUnsupported = errors.New("native system clipboard is unsupported on this platform")

func writeSystemClipboard(string) error {
	// Do not discover wl-copy, xclip, or similar helpers through PATH. The
	// caller still emits Bubble Tea's OSC 52 clipboard command.
	return errSystemClipboardUnsupported
}
