## Context

Phase 2 already routes command-running tools through a sandbox stage, but that
stage is currently `directSandbox`, an inert implementation that calls the tool
handler directly. Phase 4 filled the surrounding hard hook stages, so the
executor now has the full chokepoint order from the architecture:
before-tool hooks, permission, sandbox, tool execution, after-tool hooks.

Phase 5 fills only the sandbox slot. The loop, event stream, tool catalog,
permission flow, hook ordering, and shell tool contract must remain intact. The
command handler owns argument validation and result post-processing, while a
sandbox-provided command runner owns process creation, timeout, cancellation,
combined output, and process-group cleanup. This split keeps handler semantics
intact without allowing the handler to launch an unconfined child process.

## Goals / Non-Goals

**Goals:**

- Derive a deterministic sandbox policy from the session working directory,
  merged settings, and explicit permission context.
- Enforce that policy for every shell tool and command-declaring custom tool on
  the existing executor path.
- Use macOS `sandbox-exec` for OS-level enforcement when available.
- Provide a weaker fallback behind the same `core.Sandbox` boundary when the OS
  backend is unavailable.
- Report the active confinement level accurately to SDK callers and frontends.
- Return sandbox blocks as recoverable tool errors with enough detail for the
  model and user to react.

**Non-Goals:**

- No Linux kernel sandbox, container backend, or Docker-based isolation in v1.
- No change to hook, permission, or executor stage ordering.
- No sandboxing of read-only file tools in this phase.
- No live streaming of command output.
- No hand-authored profile language in user settings.

## Decisions

### Decision: Keep `core.Sandbox` as the integration boundary

The executor already calls `Sandbox.Run(ctx, handler, args)` only for handlers
that declare `RunsCommands()`. Phase 5 should replace `directSandbox` with a
real implementation at session construction time, not add a second executor path
or make the shell tool call sandbox internals.

Alternatives considered:

- Put sandbox logic inside the shell tool. Rejected because custom
  command-declaring tools would bypass it.
- Add sandbox branching to the loop. Rejected because the loop must stay
  frontend/tool agnostic and all tool safety belongs in the executor chokepoint.

### Decision: Make policy derivation a separate deterministic component

`internal/sandbox` should expose a policy builder that takes normalized inputs:
working directory, scratch temp root, settings grants, and permission grants. It
returns a policy containing read roots, write roots, and network mode. The
builder should canonicalize paths, deduplicate roots, and sort output so tests
can compare policies exactly.

Alternatives considered:

- Generate backend profiles directly from settings. Rejected because it would
  make policy behavior hard to test independently from `sandbox-exec` syntax.
- Let hooks or tools mutate policy directly. Rejected for v1 because the PRD
  only requires configuration and permission context as widening inputs.

### Decision: Safe baseline plus additive grants

The baseline policy is always present: project writable, scratch temp writable,
system/tooling reads allowed, project reads allowed, and network denied. Settings
and permission context can add read roots, write roots, or network access, but
they cannot remove baseline roots or weaken the deny-by-default floor.

Alternatives considered:

- Allow a fully custom policy in settings. Rejected because it could silently
  remove the safety floor and make the runtime posture harder to explain.
- Deny reads outside the project by default. Rejected for v1 usability because
  normal compilers and interpreters need system locations.

### Decision: One backend contract with reported strength

The sandbox package should choose a backend at session construction or command
execution time and report one of two strength levels: OS-enforced or
policy-based fallback. On macOS with `sandbox-exec` available, the OS backend
generates a Seatbelt profile and wraps command execution. On other platforms or
when the executable is unavailable, the fallback enforces deny intent where the
harness can check it and reports itself as weaker.

Alternatives considered:

- Fail session creation when `sandbox-exec` is unavailable. Rejected because the
  PRD requires a labeled downgrade and continued operation.
- Pretend fallback is equivalent to OS confinement. Rejected because the user
  must never be misled about protection strength.

### Decision: Preserve handler semantics through a sandbox-provided runner

A command-running tool implements the optional command-handler contract. Its
handler validates arguments, constructs one or more command specifications, calls
the runner supplied by the sandbox stage, and may post-process the returned
output. The runner is the only supported child-process launch path and owns
timeout, cancellation, process groups, combined output, and backend confinement.
Command-declaring handlers that do not implement this contract fail closed with a
readable tool error.

Alternatives considered:

- Execute the raw `command` argument directly in the sandbox. Rejected because it
  bypasses custom handler validation, transformation, and result semantics.
- Call an arbitrary handler's existing `Execute` method under `sandbox-exec`.
  Rejected because an in-process Go handler cannot be transparently moved into a
  child sandbox and could launch an unconfined process.

### Decision: Derive active tooling roots and route caches through scratch

The baseline read policy includes the active runtime toolchain, absolute `PATH`
entries, and the Go module cache in addition to fixed system roots. Sandboxed
commands receive `TMPDIR`, `TMP`, `TEMP`, `GOTMPDIR`, and `GOCACHE` values rooted
inside the policy scratch directory so normal build commands do not require a
write grant for user cache directories.

### Decision: Surface confinement status as SDK-visible metadata

Session construction should make the active sandbox level available to SDK
callers, and the run/event layer should have enough information for a frontend to
render the same truth. The exact type shape can be chosen during implementation,
but it must distinguish OS-enforced from fallback and include a readable reason
for downgrades.

Alternatives considered:

- Only log the active backend. Rejected because logs are not the event stream and
  SDK callers need runtime state.

## Risks / Trade-offs

- [Risk] `sandbox-exec` profile syntax is easy to get subtly wrong. →
  Mitigation: keep profile generation isolated, snapshot-tested, and covered by
  macOS enforcement tests for outside-write and network-deny cases.
- [Risk] Normal Go, Node, or shell tooling may need reads outside fixed system
  roots. → Mitigation: derive active runtime and `PATH` roots, keep extra reads
  configurable, and route temporary/cache writes through scratch.
- [Risk] The fallback can deny only what the harness can inspect and cannot be as
  strong as kernel confinement. → Mitigation: label fallback clearly and test the
  same deny intent separately from backend strength.
- [Risk] Permission-granted write/network access could become too broad. →
  Mitigation: grants are additive, explicit, canonicalized, and visible in the
  derived policy/status.
- [Risk] Integrating status reporting could tempt logging onto the event stream.
  → Mitigation: add typed status/metadata only; structured logs remain outside
  run events.

## Migration Plan

1. Add sandbox policy/config/status types and deterministic policy derivation in
   `internal/sandbox`.
2. Add macOS backend profile generation and execution wrapper, guarded by
   platform availability checks.
3. Add fallback backend that preserves the same `core.Sandbox` contract and
   reports weaker confinement.
4. Extend settings and `SessionConfig` to accept sandbox grants and expose active
   confinement status.
5. Wire the real sandbox stage into default session construction while leaving
   custom dispatchers untouched.
6. Add offline tests first, then macOS-specific enforcement tests gated on
   `sandbox-exec` availability.

Rollback is straightforward during implementation: restore the default executor
stage to `directSandbox` for local debugging. That rollback must not ship as the
default once Phase 5 is accepted.

## Open Questions

- Should sandbox status also be emitted once at session start in addition to the
  SDK getter?
