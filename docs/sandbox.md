# Coragent Sandbox

Phase 5 confines command-running tools behind the executor's sandbox stage.
The default posture is safe and additive: commands can write in the project and
scratch temp roots, can read the project and system tooling locations, and
cannot use the network unless network access is explicitly granted.

The baseline derives read access for the active runtime toolchain, absolute
`PATH` entries, and the Go module cache. Sandboxed commands receive temporary and
Go build-cache environment paths under the writable scratch root, so normal Go
builds do not need permission to modify user cache directories.

## Settings

Sandbox grants live in the single JSON settings file, either
`~/.coragent/settings.json` or `.coragent/settings.json`. Project settings merge
over home settings field-by-field.

```json
{
  "sandbox": {
    "extra_read_roots": ["/path/to/read"],
    "extra_write_roots": ["/path/to/write"],
    "network": false
  }
}
```

- `extra_read_roots` adds locations commands may read.
- `extra_write_roots` adds locations commands may write.
- `network` grants outbound network access when `true`; the default is denied.

These fields only widen the baseline. They do not accept raw Seatbelt profile
text, and they cannot remove the project or scratch write roots.

## Custom Command Tools

An SDK tool whose `RunsCommands()` method returns `true` must also implement
`agent.CommandToolHandler`. Its `ExecuteCommand` method keeps ownership of
argument validation and result post-processing, but launches each child process
through the supplied `agent.CommandRunner` using an `agent.CommandSpec`.

This contract makes the process launch enforceable without bypassing custom tool
logic. A command-declaring tool that has not adopted the runner contract fails
closed with a readable tool error; its ordinary `Execute` method is not called.

## Runtime Status

SDK callers can inspect `Session.SandboxStatus()`:

- `ConfinementOSEnforced` means macOS `sandbox-exec` is active and the OS is
  enforcing the generated policy.
- `ConfinementPolicyFallback` means the same sandbox boundary is active, but the
  harness is using the weaker policy fallback. The status includes a readable
  reason, such as unsupported platform or unavailable `sandbox-exec`.

The fallback is deliberately labeled as weaker. It denies identifiable forbidden
writes before unrestricted execution, but it is not equivalent to kernel-level
confinement.
