package tools

import (
	"sort"
	"strings"

	"github.com/blkcor/coragent/internal/action"
	"github.com/blkcor/coragent/internal/policy"
	"github.com/blkcor/coragent/internal/sandbox"
)

// ExecutionIdentity and PreparedCommand are owned by action so Prepared can
// carry them without creating an action -> tools import cycle. The aliases keep
// the command-tool vocabulary available from this package.
type ExecutionIdentity = action.ExecutionIdentity
type PreparedCommand = action.PreparedCommand

func computeExecutionIdentity(spec sandbox.CommandSpec, level sandbox.ConfinementLevel) action.ExecutionIdentity {
	keys := make([]string, 0, len(spec.Env))
	valuesByKey := make(map[string]string, len(spec.Env))
	for _, entry := range spec.Env {
		key, value, _ := strings.Cut(entry, "=")
		keys = append(keys, key)
		valuesByKey[key] = value
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, valuesByKey[key])
	}
	readPaths := append([]string(nil), spec.Grants.AllowedReadPaths...)
	writePaths := append([]string(nil), spec.Grants.AllowedWritePaths...)
	sort.Strings(readPaths)
	sort.Strings(writePaths)
	return action.ExecutionIdentity{
		Command: spec.Command, Args: append([]string(nil), spec.Args...), CWD: spec.CWD,
		EnvKeys: keys, EnvValues: values, Timeout: spec.Timeout,
		MaxOutputBytes: spec.MaxOutputBytes, PTY: spec.PTY,
		ReadPaths: readPaths, WritePaths: writePaths, Network: spec.Grants.Network,
		SandboxLevel: level, PolicyVersion: policy.PolicyVersion,
	}
}
