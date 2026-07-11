# Sandbox

## Purpose

OS-level and policy-based sandbox confinement for command-executing tools. Derives sandbox policies from session context and settings, enforces them via macOS `sandbox-exec` where available, and falls back to policy-based enforcement on unsupported platforms.

## Requirements

### Requirement: Deterministic policy derivation
The system SHALL derive each shell-command sandbox policy from the session
working directory, scratch temporary root, sandbox settings, and explicit
permission context. The derivation MUST canonicalize and deduplicate paths so the
same inputs produce the same policy every time.

#### Scenario: Baseline policy is derived automatically
- **WHEN** a session has a working directory and no sandbox grants
- **THEN** the derived policy makes the working directory writable
- **THEN** the derived policy makes a scratch temporary root writable
- **THEN** the derived policy permits reads needed for project files and normal system tooling
- **THEN** the derived policy denies network access

#### Scenario: Ordinary Go tooling uses the safe baseline
- **WHEN** a sandboxed command runs the active Go toolchain with no extra grants
- **THEN** the active executable, runtime toolchain, and module-cache locations are readable
- **THEN** temporary files and the Go build cache are written under the scratch root
- **THEN** an ordinary dependency-free `go test` succeeds without widening host write roots

#### Scenario: Fixed inputs produce identical policy
- **WHEN** policy derivation is called repeatedly with the same working directory, scratch temporary root, settings, and permission context
- **THEN** each derived policy is identical

### Requirement: Safe baseline floor
The system SHALL preserve a safe baseline floor in every derived policy:
project writes and scratch writes are allowed, writes outside allowed write roots
are denied, reads are broader than writes, and network access is denied unless
explicitly granted.

#### Scenario: Read permission does not imply write permission
- **WHEN** a path is allowed only by the policy read roots
- **THEN** the policy permits reading that path
- **THEN** the policy denies writing that path

#### Scenario: Grants never remove baseline roots
- **WHEN** settings or permission context add read roots, write roots, or network access
- **THEN** the derived policy still includes the baseline project write root
- **THEN** the derived policy still includes the baseline scratch write root

### Requirement: Additive sandbox grants
Sandbox settings and explicit permission context SHALL only widen the baseline
policy by adding readable roots, writable roots, or network access. They MUST NOT
remove baseline permissions or disable the deny-by-default posture for unrelated
paths.

#### Scenario: Extra write root is added
- **WHEN** configuration or permission context grants an extra write root
- **THEN** commands under that sandbox policy can write inside that extra root
- **THEN** writes outside all allowed write roots remain denied

#### Scenario: Extra read root is added
- **WHEN** configuration grants an extra read root
- **THEN** commands under that sandbox policy can read inside that extra root
- **THEN** the default read roots remain unchanged except for the explicit addition

#### Scenario: Network grant is explicit
- **WHEN** no configuration or permission context grants network access
- **THEN** the derived policy denies network access
- **WHEN** configuration or permission context grants network access
- **THEN** the derived policy permits network access

#### Scenario: Per-call grants preserve existing permission context
- **WHEN** the initial permission context grants a root and a later approved call adds another root or network access
- **THEN** the effective call policy contains both the initial and per-call grants
- **THEN** the base session policy remains unchanged after the call

### Requirement: macOS OS-level enforcement
On macOS, when the OS sandbox backend is available, the system SHALL run
command-executing tools under a `sandbox-exec` confinement profile that enforces
the derived read, write, and network policy at the operating-system level.

#### Scenario: In-project write succeeds on macOS
- **WHEN** a sandboxed shell command writes inside the working directory on macOS with the OS backend active
- **THEN** the command succeeds
- **THEN** the written file exists inside the working directory

#### Scenario: Outside write is blocked by the OS backend
- **WHEN** a sandboxed shell command writes outside all allowed write roots on macOS with the OS backend active
- **THEN** the operating-system sandbox blocks the write
- **THEN** the target file is not created
- **THEN** the tool result is an error result the model can read

#### Scenario: Network is denied by default on macOS
- **WHEN** a sandboxed shell command attempts an outbound network connection on macOS with the OS backend active and no network grant
- **THEN** the operating-system sandbox blocks the connection
- **THEN** the tool result is an error result containing the captured failure

### Requirement: Fallback confinement
When an OS-level sandbox backend is unavailable, the system SHALL apply a weaker
policy-based fallback behind the same sandbox boundary. The fallback MUST deny
the same forbidden write intent to the extent the harness can enforce it and
MUST identify itself as weaker than OS-level confinement.

#### Scenario: Unsupported platform downgrades without crashing
- **WHEN** a session starts on a platform without the OS sandbox backend
- **THEN** the session remains usable
- **THEN** the active confinement level is reported as policy-based fallback
- **THEN** the system does not panic

#### Scenario: Fallback denies forbidden write intent
- **WHEN** a fallback-confined command is identified as writing outside all allowed write roots
- **THEN** the fallback denies the command before unrestricted execution
- **THEN** the tool result is an error result explaining the sandbox denial

### Requirement: Active confinement reporting
The system SHALL report the active confinement level at runtime and MUST NOT
claim stronger protection than is actually in force. The report MUST distinguish
OS-enforced confinement from policy-based fallback and include a readable reason
when fallback is active.

#### Scenario: OS backend reports OS enforcement
- **WHEN** the macOS OS sandbox backend is selected and available
- **THEN** the active confinement report says OS-enforced confinement is active

#### Scenario: Fallback report includes downgrade reason
- **WHEN** the policy-based fallback is selected
- **THEN** the active confinement report says fallback confinement is active
- **THEN** the report includes a readable downgrade reason

### Requirement: Recoverable sandbox errors
The sandbox SHALL surface blocked, cancelled, and timed-out commands as clear
recoverable tool errors. It MUST NOT crash the harness, silently succeed, or
return an error detached from the originating tool call.

#### Scenario: Blocked command is recoverable
- **WHEN** the sandbox blocks a command
- **THEN** the executor returns a tool result with `IsError` true
- **THEN** the result text explains that the sandbox blocked the command
- **THEN** the run can continue so the model can recover

#### Scenario: Cancelled command stops child work
- **WHEN** cancellation reaches a command running under the sandbox
- **THEN** the command and its child process group are stopped promptly
- **THEN** any output produced so far is returned in the error result

#### Scenario: Timed-out command stops child work
- **WHEN** a command running under the sandbox exceeds its time budget
- **THEN** the command and its child process group are stopped promptly
- **THEN** any output produced so far is returned in the error result

### Requirement: Profile generation is internal
The system SHALL generate backend-specific sandbox profiles from the derived
policy internally. User settings MUST NOT require or accept hand-authored
backend profile text in v1.

#### Scenario: Settings provide grants, not profile text
- **WHEN** sandbox settings are loaded
- **THEN** they provide extra read roots, extra write roots, and network grants
- **THEN** they do not require backend-specific profile syntax

#### Scenario: Generated profile reflects policy
- **WHEN** the macOS backend receives a derived policy
- **THEN** the generated sandbox profile allows the policy read roots
- **THEN** the generated sandbox profile allows the policy write roots
- **THEN** the generated sandbox profile applies the policy network mode
