# Coragent V2 Product Contract

## Product statement

Coragent V2 is a terminal coding agent for a single developer working inside a
source repository. Given a repository task, it reads the applicable project
instructions, investigates the code, proposes and performs authorized changes,
runs verification, and records enough state to resume or audit the session.

The runtime around the model is the product. Model access alone does not make a
useful coding agent. Coragent must manage context, tools, permissions, recovery,
session state, and interaction well enough to complete real work repeatedly.

## Target user

The first user is a developer who wants a local daily driver in the terminal and
is willing to configure an OpenAI-compatible model endpoint. V2 optimizes for
one person, one active repository, and one active session before it addresses
team workflows or broad embedding use cases.

The first GA certification targets macOS because V2 requires enforceable process
confinement for command execution. Other platforms are not GA-supported until a
backend can enforce the same Authority Envelope.

## Primary job

The user should be able to open a repository, describe an outcome, and let
Coragent carry the task through this sequence:

1. Load the instructions that apply to the working directory.
2. Inspect enough code to form a grounded plan.
3. Use read-only tools without unnecessary interruption.
4. Prepare mutations and commands before any side effect occurs.
5. Ask for approval with an exact patch or action description.
6. Apply the approved action through the same enforced boundary.
7. Run relevant verification and respond to failures.
8. Report what changed, what was verified, and what remains uncertain.

Long tasks must survive context pressure and process restarts. The user must be
able to steer or cancel work without corrupting the transcript or leaving child
processes behind.

## Why V2 exists

V1 established useful engineering ideas, including one execution chokepoint,
cancellation propagation, offline provider fakes, prepared edits, and a frontend
boundary. It also stabilized a broad SDK, two event protocols, detailed
permission machinery, and a full TUI before real coding-task success became a
release gate.

V2 keeps the proven ideas and removes the compatibility burden. It validates
agent behavior before publishing a public SDK or rebuilding advanced interface
features.

## Goals

### Complete repository tasks

Coragent must answer codebase questions, make focused changes, repair seeded
failures, and recover from tool problems. File citations, diffs, commands, and
test results must come from the active workspace.

### Keep authority narrow and visible

Read-only operations inside the workspace run without approval. Mutations and
commands require a prepared preview and user approval. Workspace escape,
unapproved network use, stale patches, and alternate execution paths fail
closed.

### Preserve long-task coherence

The runtime stores a durable transcript and creates a separate context view for
the model. It offloads large results, compacts old material, and restores the
current goal, constraints, task state, and unresolved tool work after
compaction.

### Support interactive control

The user can submit work, answer approval requests, queue steering, cancel active
work, and resume a saved session. The same SessionCommands work from the
line-oriented CLI, the later TUI, and the eventual SDK.

### Make behavior measurable

Offline invariant tests protect runtime correctness. A fixed 12-task benchmark
measures repeatable coding behavior with a real model. A separate held-out suite
checks that Mercury-specific tuning generalizes across repositories and language
ecosystems. Release decisions use all three forms of evidence.

## Initial non-goals

The first V2 release does not include:

- V1 source, wire, or session-data compatibility
- a public Go SDK before M5
- provider families beyond one OpenAI-compatible adapter
- subagents, persistent teammates, or parallel tool execution
- MCP, plugins, a plugin marketplace, or remote service connectors
- external command hooks or a general hook extension system
- persistent remembered approvals or multiple permission modes
- skills and slash-skill execution
- a web interface
- telemetry, billing, or hosted account management
- a feature-rich TUI with themes, mouse workflows, animation, or a capability
  inspector
- process actions or GA support on platforms without an enforcing sandbox
- perfect detection of arbitrary confidential values that are neither labeled
  nor matched by the versioned credential detector

These features require a product or benchmark reason. Package seams alone do not
justify them.

## Product principles

### Product evidence beats abstraction

Coragent publishes its SDK only after the terminal product passes the benchmark.
Before then, internal interfaces may change when task evidence shows a problem.

### The user sees the action that will occur

Mutation approval is tied to the prepared patch or command. If the underlying
state changes, the approval is invalid and the runtime prepares a new action.

### Durable history is not model context

The transcript remains complete. Context compaction changes what the model sees,
not what Coragent records. A summary never replaces its source records.

### Safety claims must match enforcement

Path checks, command classification, and worktrees are useful controls, but they
are not kernel confinement. The product reports the active enforcement level and
does not relabel an unrestricted fallback as a sandbox.

### Sensitive data uses explicit projections

Runtime credentials never enter model, transcript, Event, log, tool, process, or
artifact data. Protected-path and detected credential content is redacted before
those boundaries. Coragent reports this defined guarantee and does not claim
that heuristics discover every unlabeled secret.

### Failure is part of the workflow

Tool errors return to the model when recovery is possible. Provider and context
failures follow bounded recovery paths. Corrupted durable state and violated
protocol invariants stop the run with a clear cause.

## Product surfaces

V2 introduces product surfaces in this order:

1. A line-oriented CLI that proves the session and tool behavior.
2. A minimal TUI that projects the same SessionCommands, transcript, and Events.
3. A public Go SDK after both frontends pass the same conformance tests.

The runtime does not import either frontend. The TUI does not maintain a second
copy of session truth.

## Data and rollback boundary

V2 uses the same settings paths as V1: `~/.coragent/settings.json` for
home-scoped settings and `.coragent/settings.json` for project-local settings.
The settings format stays readable by the V1 binary so both binaries can share
the file during the transition. Durable session state lives under
`~/.coragent/sessions/`.

The initial V2 release performs no V1 data migration. Users can switch back to
the V1 binary without restoring files or rewriting settings. After V2 becomes
the default binary, V1 remains available as a tagged release for one release
cycle.

## General availability criteria

V2 is ready to replace V1 when all of these conditions hold:

- A new user can configure a model and receive a grounded repository answer in
  less than five minutes.
- The pinned 12-task benchmark runs for three suite rounds, producing 36 scored
  slots, and at least 29 slots pass.
- Every benchmark task passes at least two of its three slots.
- The same Coragent commit and immutable reference model profile pass the
  held-out generalization gate in `docs/benchmarks.md`.
- No physical benchmark execution meets a `safety_fail` condition in
  `docs/benchmarks.md`. That classification takes precedence over task, runtime,
  and infrastructure outcomes and cannot be removed by a rerun.
- Runtime credentials and content classified as sensitive never appear
  unredacted in Transcript, Model Context, Events, logs, blobs, or benchmark
  artifacts.
- A session can resume after process exit with the same transcript, task state,
  workspace baseline, Run Budget, and reconciled Action Attempts. It never
  automatically repeats an uncertain mutation or process action.
- A scripted task can exceed two model context windows and still complete after
  automatic compaction.
- Cancellation stops the model stream, active tool, and process group without
  orphan work.
- Steering submitted during a run is applied at the next documented safe
  boundary.
- State-machine, transcript, context, action, permission, recovery, and
  persistence behavior pass deterministic offline tests and race tests.
- The TUI passes the 36-slot GA benchmark. CLI and TUI pass the same deterministic
  SessionCommand, Event, and transcript conformance suite without importing
  internal runtime packages after the M5 SDK cutover.

The GA claim certifies the Coragent commit and immutable reference model profile
recorded in the benchmark report. Users may configure other compatible
endpoints, but compatibility does not imply the same measured quality.

## Premise check

V2 assumes that a single agent with better prompt assembly, context management,
tools, and recovery can reach daily-driver quality. M2 tests this assumption.

M2 may proceed only when at least 26 of 36 scored slots pass, investigation and
focused-edit tasks each pass at least 8 of 12 slots, repair and recovery tasks
each pass at least 4 of 6 slots, and no safety failure occurs. A score of 25 or
fewer collapses the current product premise. A higher total with a missed
category floor leaves the premise unproven. Either outcome pauses M3 and all
later product work, including the TUI, SDK, subagents, and integrations. Failure
remediation and rerunning the same M2 gate continue.

The team must identify whether failures come from the loop, prompt, context,
tool contract, or model adapter and improve that layer before resuming the
roadmap.

Subagents enter planning only after the single-agent release gate passes and
real sessions show failures caused by context isolation or independent parallel
investigation. They do not compensate for a weak core loop.
