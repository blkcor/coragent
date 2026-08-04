# Coragent V2 Roadmap

Coragent V2 grows through five product milestones. Each milestone is runnable,
useful, and safe without the milestone after it. The implementation starts with
a terminal product and publishes an SDK only after the product contracts have
survived real repository work.

The documentation baseline is a design contract, not an investigation phase.
M1 is the first implementation milestone and ships a usable read-only product.

## Delivery rules

- Build vertical product slices. Do not land a layer that has no user-visible or
  test-visible behavior until a later milestone.
- Keep runtime code internal through M4.
- Run the unlocked benchmark subset in every milestone.
- Preserve earlier product scenarios as regression tests even when internal
  contracts change.
- Stop a milestone when a safety gate fails. Warnings do not substitute for
  enforcement.
- Do not add V1 compatibility paths to make migration appear cheaper.
- Keep V2's versioned records distinguishable from unknown legacy state and
  leave unrecognized data untouched.

## Milestone summary

| Milestone | Product result | Benchmark scope |
| --- | --- | --- |
| M1: Read-only Repo Companion | grounded repository investigation with durable sessions | four investigation tasks |
| M2: Safe Change Loop | reviewed edits and sandboxed verification | full 12-task suite |
| M3: Durable Long Tasks | context compaction, recovery, and restart continuity | full suite plus context stress |
| M4: Interactive Daily Driver | steering, cancellation, session navigation, and focused TUI | TUI score suite plus frontend conformance |
| M5: Evidence-gated SDK and Cutover | supported V2 SDK and default product release | release gate and conformance suite |

## M1: Read-only Repo Companion

### User value

A developer can ask questions about a repository, receive answers grounded in
real files, leave the process, and resume the same session later. If development
stops after M1, Coragent remains a useful read-only repository companion.

### Scope

- a line-oriented `coragent` CLI
- an internal Session state machine
- one serializable SessionCommand protocol and one serializable Event protocol
- an append-only durable transcript
- session creation, listing, loading, resuming, and closing
- one OpenAI-compatible streaming provider adapter
- explicit provider capabilities and context-window configuration
- cancellation of the active provider stream
- bounded retry for rate limits and transient transport failures, including
  `Retry-After`
- a durable Run Budget for model calls, transport attempts, and retry delay
- runtime prompt assembly
- deterministic discovery of `CLAUDE.md` and `AGENTS.md`
- a dedicated provider credential source plus normal, sensitive, and
  runtime-secret data projections
- workspace-scoped list, read, and search tools
- pure-Go M1 file tools that launch no helper process
- one Action Broker even though all M1 tools are read-only
- a scripted fake provider and temporary session store
- the four investigation benchmark tasks

### Acceptance gate

- From a clean checkout, a user can configure the provider and receive a
  grounded repository answer in less than five minutes.
- Answers to the benchmark cite the files and line ranges used.
- The model cannot request mutation, command execution, network access, or reads
  outside the workspace.
- A saved session resumes with the same transcript and can accept another user
  turn.
- Replaying a transcript produces the same user-visible conversation.
- Every session emits monotonic Event cursors, every run emits exactly one
  terminal Event, and atomic observation cannot lose an active interaction.
- Provider and product tests pass without network access.
- Cancellation closes the stream and leaves no active provider work.
- Restart cannot reset a run's model-call, transport-attempt, or retry-delay
  counters.
- Runtime credentials never enter Transcript, Model Context, Events, logs, or
  artifacts; protected and detected workspace content is redacted.

### Failure behavior

Malformed provider streams, invalid durable records, and permanent provider
errors stop the run with a typed cause. Missing optional instruction files are
normal. Conflicting instruction sources follow the precedence in
`architecture.md` and record their provenance.

### Replacement boundary

M1 writes session state directly under `~/.coragent/sessions/` and retains the
established settings paths. V1 is archived and is not a rollback target.
Replacing an M1 binary never migrates, rewrites, or deletes unknown session
entries automatically.

## M2: Safe Change Loop

### User value

A developer can ask Coragent to investigate a small problem, review an exact
patch or command, approve it, run verification, and receive a grounded result.
M2 is the first milestone that can complete everyday code changes.

### Scope

- one prepared patch tool for file creation and modification
- identity-bound previews and stale-source detection
- one command tool with timeout, process-group cancellation, and bounded output
- an immutable per-session Authority Envelope plus Effective Policy
- a workspace-scoped filesystem service for every file tool
- macOS OS-level command sandboxing
- a process-group supervisor that kills children when the Coragent control
  channel closes
- a disabled command tool on platforms without equivalent confinement
- a minimal process environment with no ambient credential variables
- correlated approval SessionCommands for mutations and commands
- an acknowledgement SessionCommand for an indeterminate recovered action
- argument revision that invalidates the old preview and requires preparation
  and approval again
- per-call read-root, write-root, and network grants
- a durable Action Attempt journal and crash reconciliation
- Run Budget extensions for tool-call count and active process time
- credential detection for prepared patches and process output before preview or
  projection
- result persistence when output exceeds the model-facing budget
- complete tool-call pairing for success, failure, denial, policy block,
  cancellation, stale state, prior-result skips, and crash reconciliation
- full 12-task benchmark support

### Initial permission posture

- workspace reads are allowed
- workspace mutations ask with an exact prepared patch
- commands ask with their effective command, minimal environment, and grants
- outside-workspace roots and network endpoints must be configured in the
  Authority Envelope when the session starts
- a per-call grant activates only a narrow subset of the Authority Envelope
- approvals expire with the prepared action

M2 does not persist remembered decisions or expose alternate permission modes.

### Acceptance gate

- Coragent completes an investigation, reviewed edit, verification command, and
  final report from one user request.
- The displayed patch and committed patch are byte-for-byte identical.
- A file changed after preview cannot be overwritten by the stale prepared
  action.
- Absolute paths, `..`, symlinks, and rename races cannot escape the approved
  workspace and grants.
- An unapproved action performs no side effect.
- Cancelling a command terminates the full process group.
- Crashes at every Action Attempt boundary reconcile file state, never replay a
  process action, and produce exactly one terminal ToolResult.
- The full benchmark runs as three 12-slot rounds through the line-oriented CLI,
  with recorded transcripts, diffs, command outputs, and safety results.
- No execution meets a `safety_fail` condition from `docs/benchmarks.md`,
  regardless of aggregate score or an infrastructure rerun.

### Premise gate

M2 proceeds only when at least 26 of 36 scored slots pass, investigation and
focused-edit tasks each pass at least 8 of 12 slots, repair and recovery tasks
each pass at least 4 of 6 slots, and no safety failure occurs. A score of 25 or
fewer collapses the product premise. A higher total with a missed category floor
leaves it unproven. Either result pauses M3 and later product work while failures
are assigned to the loop, prompt, context, provider, or tool contract and
corrected.

Do not start subagent or TUI work to compensate for a failed M2 core.

### Rollback

Prepared actions are single-use and session records are append-only. Rolling back
the binary does not replay approved actions. Repository changes remain ordinary
user-reviewed Git changes.

## M3: Durable Long Tasks

### User value

Coragent can work beyond one model context window, recover from common provider
failures, and resume after process exit without losing the current goal or
repeating committed actions.

### Scope

- separate durable transcript, structured task ledger, and model context view
- a session-owned blob store for large normal or already-redacted tool results
- deterministic pruning and reloadable tool-result references
- proactive summary checkpoints with transcript range provenance
- one bounded reactive compaction path for provider context overflow
- post-compaction restoration of policy, project instructions, current goal,
  constraints, pending tool state, task ledger, and recent file context
- explicit recovery state for rate limits, overload, output truncation, context
  overflow, malformed streams, and cancellation
- bounded output-budget escalation and continuation
- Run Budget extensions for compaction, continuation, token, and total active
  time limits
- crash-safe transcript, Action Attempt, budget, checkpoint, and task-ledger
  persistence
- context usage and recovery events for frontends

### Acceptance gate

- A scripted scenario accumulates more than two configured context windows and
  still completes the requested repository task.
- The long-context scenario is an independent non-scoring gate and does not add
  slots to the fixed 36-slot benchmark.
- Compaction preserves current goals, constraints, open tool pairs, task state,
  and required project instructions.
- Source transcript records remain available after every checkpoint.
- Exiting immediately before or after compaction produces the same resumed task
  state.
- Resume does not repeat a committed patch or command.
- Resume retains Run Budget counters and reconciles every started Action Attempt
  before another model request.
- Recovery tests prove that every retry consumes a counter and stops at its
  configured bound.
- A malformed summary cannot replace structured constraints or task state.

### Failure behavior

If proactive compaction fails, the current valid context remains active until a
provider limit requires reactive compaction. If reactive compaction also fails,
the run stops with the transcript intact. Coragent never deletes source history
to make a request fit.

### Rollback

M3 adds new V2 record kinds. A V2 binary that does not understand a newer or
legacy record stops read-only with a version error instead of rewriting the
session.

## M4: Interactive Daily Driver

### User value

A developer can use Coragent throughout the day, steer active work, cancel it,
switch sessions, inspect patches and tool state, and resume previous work from a
focused terminal interface.

### Scope

- queued steering applied at documented safe boundaries
- queued next prompts
- immediate cancellation separate from steering
- a minimal Bubble Tea TUI
- transcript rendering
- active tool and command status
- prepared patch and permission views
- redaction notices that never reveal the matched value
- task-ledger and context-pressure views
- session creation, selection, resume, and close
- narrow-terminal plain-text fallback
- the line-oriented CLI retained as a supported diagnostic frontend

M4 does not rebuild V1 themes, mouse selection, animation, complex Markdown,
slash-skill menus, or a capability inspector.

### Acceptance gate

- Steering submitted during provider or tool work appears before the next model
  request after a safe boundary.
- Remaining tool calls skipped because of steering receive explicit skipped
  results.
- Cancellation returns the UI to input state and leaves no model stream, tool,
  or process running.
- Loading a saved session renders the same semantic transcript as the CLI.
- Permission can always be denied at the minimum supported terminal size.
- The TUI becomes the scoring frontend and completes a new 36-slot benchmark
  report. The CLI and TUI also produce the same runtime outcomes in the
  deterministic SessionCommand, Event, and transcript conformance suite.
- Frontend tests prove that neither frontend imports runtime internals or owns a
  second session state machine.

### Rollback

The line-oriented CLI remains available if the TUI cannot run in a terminal.
Both frontends use the same V2 session data.

## M5: Evidence-gated SDK and Cutover

### User value

Coragent becomes both the default terminal product and a Go runtime that another
program can embed without depending on internal packages.

### Scope

- a new major Go SDK designed from M1 through M4 behavior
- a deliberately small public vocabulary chosen during the M5 API review from
  product-proven concepts; no pre-M5 internal type name is binding
- SDK-owned public data types with no internal aliases
- no embedded channels or callbacks in serializable events
- Tool and Provider conformance suites
- CLI and TUI migrated to use only the public SDK
- one small one-shot example frontend
- versioning and compatibility policy beginning with V2
- installation, upgrade, binary-replacement, and V1 incompatibility documentation
- release packaging for platforms with verified behavior

### Acceptance gate

- At least 29 of 36 scored slots pass, and each task passes at least two of its
  three slots.
- The immutable GA reference model profile and held-out generalization suite pass
  the gates in `docs/benchmarks.md`.
- No execution meets a `safety_fail` condition from `docs/benchmarks.md`.
- CLI, TUI, and the example frontend import only the SDK.
- Every exported API is exercised by Coragent or the concrete example.
- SDK types contain no aliases to internal runtime types.
- Provider and Tool conformance suites pass against the built-in adapters and
  fakes.
- The full offline, race, lint, build, macOS, and packaging gates pass.
- The maintainer approves the API support burden before the first stable tag.

### Cutover and replacement

The V2 binary becomes the default `coragent`. V1 remains archived and is not a
supported rollback target. Replacing the installed V2 binary leaves versioned
session data intact and never deletes or migrates unknown legacy entries.

## Conditional work after the V2 release

Subagents are considered only when the single-agent GA gate passes and recorded
daily-driver sessions show failures caused by context isolation or independent
parallel investigation. Their design must include fresh context, narrowed tools,
permission bubbling, parent cancellation, result-only return, and worktree
isolation for mutating work.

Persistent teams require a separate product case, durable task graph, typed
mailbox protocol, atomic claiming, and workspace isolation. MCP requires the M5
Tool contract and a separate security review. Neither capability is implied by
the initial package layout.
