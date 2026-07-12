//go:build darwin

package tui

import (
	"os/exec"
	"strings"
)

// Use the platform-owned absolute path. Clipboard writes happen on ordinary
// mouse drags, so resolving a helper through a caller-controlled PATH would
// turn selection into implicit execution of an untrusted binary.
const systemClipboardProgram = "/usr/bin/pbcopy"

func writeSystemClipboard(text string) error {
	command := exec.Command(systemClipboardProgram)
	command.Stdin = strings.NewReader(text)
	return command.Run()
}
