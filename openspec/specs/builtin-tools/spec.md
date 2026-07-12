# builtin-tools Specification

## Purpose
TBD - created by archiving change phase-2-tools-executor. Update Purpose after archive.
## Requirements
### Requirement: Read a file, optionally a window

The read-file tool SHALL return a file's contents line-referenced so the model can
cite lines, and SHALL support an optional line offset and limit returning only
that window. It SHALL return a clear error — dumping no raw bytes — for a path
that is missing, a directory, unreadable, binary, or an offset past end of file.

#### Scenario: Whole-file read is line-referenced

- **WHEN** a file is read with no window
- **THEN** its contents are returned line-referenced so the model can cite lines

#### Scenario: Windowed read returns only the window

- **WHEN** a file is read with a line offset and limit
- **THEN** only that window of lines is returned

#### Scenario: Bad read targets return a clean error

- **WHEN** the path is missing, a directory, unreadable, binary, or the offset is past end of file
- **THEN** a clear error is returned and no raw bytes are dumped

### Requirement: Create or replace a file

The write-file tool SHALL create or replace a file by path, optionally create
missing parent folders, and return a concise confirmation that does not echo the
content.

#### Scenario: Write with parent creation scaffolds folders

- **WHEN** a file is written with parent-creation enabled and missing parent folders
- **THEN** the missing folders are created and the file is written

#### Scenario: Write replaces existing contents

- **WHEN** a file is written to an existing path
- **THEN** its contents are replaced and the result is a concise confirmation that does not echo the content

### Requirement: Surgical, unambiguous edits

The precise-edit tool SHALL replace a snippet that appears exactly once in place
and write the file. A snippet appearing more than once SHALL be rejected as
ambiguous, reporting the match count, with the file left byte-for-byte unchanged,
unless replace-all is explicitly opted into — in which case every occurrence SHALL
be replaced. A missing target or a no-op (target equals replacement) SHALL be
rejected with a clear reason and the file left unchanged.

#### Scenario: Unique snippet is replaced

- **WHEN** the target snippet appears exactly once
- **THEN** it is replaced in place and the file is written

#### Scenario: Ambiguous snippet is rejected unchanged

- **WHEN** the target snippet appears more than once and replace-all is not opted into
- **THEN** the edit is rejected as ambiguous, the match count is reported, and the file is left byte-for-byte unchanged

#### Scenario: Replace-all replaces every occurrence

- **WHEN** the target snippet appears more than once and replace-all is explicitly opted into
- **THEN** every occurrence is replaced

#### Scenario: Missing target or no-op is rejected unchanged

- **WHEN** the target is not present in the file, or the replacement is identical to the target
- **THEN** the edit is rejected with a clear reason and the file is left unchanged

### Requirement: Run commands, see everything

The shell tool SHALL return a command's combined output and exit code, report a
non-zero exit as an error result carrying the captured output, and enforce a time
budget. A command exceeding its budget SHALL be killed and reported as timed out
with any partial output, leaving no orphan process. A cancelled command SHALL
return a cancellation error result with its child process stopped.

#### Scenario: Combined output and exit code returned

- **WHEN** a command runs to completion
- **THEN** its combined output and exit code are returned

#### Scenario: Non-zero exit is an error result

- **WHEN** a command exits non-zero
- **THEN** an error result with the captured output is returned, not a crash

#### Scenario: Timeout kills the command with partial output

- **WHEN** a command exceeds its time budget
- **THEN** it is killed, the result notes the timeout with any partial output, and no orphan process is left behind

#### Scenario: Cancellation stops the child process

- **WHEN** the surrounding work is cancelled mid-command
- **THEN** the call returns a cancellation error result, the child process is stopped, and nothing leaks

### Requirement: Find text across the project

The content-search tool SHALL return results as `file:line:match` lines, scopable
by path, by file glob, and by case sensitivity. A search matching nothing SHALL
return a successful "no matches" result, not an error. When the backend search
binary is unavailable, the tool SHALL degrade to a clear, actionable error, never
a crash.

#### Scenario: Matches return located results

- **WHEN** a search over a directory containing a known string runs
- **THEN** results are returned as `file:line:match` lines, scopable by path, file glob, and case sensitivity

#### Scenario: No matches is a successful answer

- **WHEN** a content search matches nothing
- **THEN** a successful "no matches" result is returned, not an error

#### Scenario: Missing backend binary degrades cleanly

- **WHEN** the backend content-search binary is unavailable
- **THEN** a clear, actionable error is returned rather than a crash

### Requirement: List files by name pattern

The file-pattern-search tool SHALL return matching paths in stable order with
common noise directories (such as version-control internals) skipped, treat no
matches as a successful "no files matched" result, and return an error for a bad
root.

#### Scenario: Matching paths returned in stable order

- **WHEN** a glob is searched under a valid root
- **THEN** matching paths are returned in stable order with common noise directories skipped

#### Scenario: No matches is a successful answer

- **WHEN** a glob matches nothing
- **THEN** a successful "no files matched" result is returned

#### Scenario: Bad root returns an error

- **WHEN** the search root is invalid
- **THEN** an error is returned

### Requirement: Built-ins declare their action classification

Each built-in tool SHALL declare its action classification — read-only,
edits-files, or runs-commands — so the permission stage can gate the correct set
of actions in plan mode and auto-accept-edits mode. The read, content-search, and
file-find tools SHALL classify as read-only; the write and edit tools SHALL
classify as edits-files; the shell tool SHALL classify as runs-commands.

#### Scenario: Read, search, and find classify as read-only

- **WHEN** the action classification of the read, content-search, or file-find tool is inspected
- **THEN** it is read-only

#### Scenario: Write and edit classify as edits-files

- **WHEN** the action classification of the write or edit tool is inspected
- **THEN** it is edits-files

#### Scenario: Shell classifies as runs-commands

- **WHEN** the action classification of the shell tool is inspected
- **THEN** it is runs-commands

### Requirement: Delegate a task to a child agent
When default session composition owns the `task` name, its built-in toolset SHALL
include a `task` tool whose arguments are a non-blank string `label`, a non-blank
string `instruction`, and an optional array of string tool names. The tool SHALL
delegate through the subagent orchestrator and SHALL return its outcome as an
ordinary tool result.

#### Scenario: Task descriptor is advertised by default
- **WHEN** a session uses the default executor and derived catalog descriptors with no caller-owned `task` handler
- **THEN** the provider is offered one `task` descriptor in addition to the existing built-in descriptors

#### Scenario: Task accepts an explicit tool list
- **WHEN** the model calls `task` with a label, instruction, and string-array tool list
- **THEN** the handler passes the parsed request to the orchestrator for restricted child construction

#### Scenario: Task accepts omitted tools
- **WHEN** the model calls `task` with a valid label and instruction but no tool list
- **THEN** the handler delegates with the safe read-only default selection

#### Scenario: Blank text arguments are rejected
- **WHEN** the model calls `task` with a missing, empty, or whitespace-only label or instruction
- **THEN** the call returns a recoverable argument error and no child starts

#### Scenario: Invalid tool-list shape is rejected
- **WHEN** the model supplies `tools` as a non-array value or with a non-string element
- **THEN** argument validation rejects the call before the orchestrator starts a child

### Requirement: Task is a non-command orchestration action
The `task` handler SHALL declare that it does not directly run commands and SHALL
be classified as read-only orchestration for the parent permission stage. Any
tool invoked by the child MUST still receive its own action classification and
permission decision.

#### Scenario: Plan mode permits read-only delegation but blocks child mutation
- **WHEN** a plan-mode parent invokes `task` and the child later attempts a mutating tool
- **THEN** the task may start, while the child mutation is independently blocked by the existing plan-mode rule

#### Scenario: Task skips the command sandbox stage
- **WHEN** the executor reaches the handler execution slot for `task`
- **THEN** the task handler runs directly and only command-running tools selected by the child route through the sandbox stage

### Requirement: Caller-owned task execution remains compatible
Default session composition MUST preserve an existing caller handler named
`task` instead of registering the standard subagent handler over it. A
caller-supplied dispatcher SHALL remain unmodified and SHALL receive no automatic
task or child-session wiring. An explicit non-nil advertised tool list SHALL
continue to determine whether any executable `task` handler is offered to the
provider.

#### Scenario: Existing custom task handler retains ownership
- **WHEN** a default-executor session supplies a custom handler named `task`
- **THEN** construction does not panic or replace it, the custom handler remains executable, and the standard subagent handler is not installed

#### Scenario: Explicit descriptors can hide task
- **WHEN** a default-executor session supplies a non-nil advertised tool list that omits `task`
- **THEN** the provider is not offered `task`, and a child cannot recover it from the parent's effective advertised tool set

#### Scenario: Custom dispatcher remains untouched
- **WHEN** a session supplies its own dispatcher and advertised tools
- **THEN** the harness uses them as-is and does not install or advertise the standard task capability automatically

### Requirement: Write and edit prepare exact file candidates
The `write_file` and `edit_file` built-ins SHALL implement side-effect-free action
preparation. Each prepared action MUST derive one exact candidate file state from
the effective arguments and the target snapshot. Files and candidates MUST stay
within a fixed safe byte limit. Within the diff-computation budget, its structured
diff SHALL compare the full before state with that candidate before preview
bounding is applied; beyond that computation budget, it SHALL return a typed
metadata-only preview with known before/candidate byte sizes and an explicit
`preview_budget` omission rather than allocate unbounded diff state. Preparation
MUST NOT create requested parent directories or write the target. Commit SHALL
apply that prepared candidate only while its captured parent, type, no-follow
link state, stable target identity, and preimage precondition remain valid at the
final revalidation before atomic replacement. Existing targets
MUST have exactly one hard link at preparation and commit; otherwise the built-in
SHALL fail closed without attempting to enumerate or modify aliases.

#### Scenario: New file preview contains all additions
- **WHEN** `write_file` prepares a path that does not exist
- **THEN** the operation is classified as create and, when within the computation budget, the complete diff treats every candidate line as an addition from an empty before state
- **THEN** neither the file nor missing parent directories are created during preparation

#### Scenario: Existing file write previews full replacement
- **WHEN** `write_file` prepares an existing path with replacement content
- **THEN** when within the computation budget, the complete diff compares the entire existing file with the entire replacement candidate
- **THEN** unchanged regions and every changed region are derived from those two full states rather than from an argument excerpt

#### Scenario: Unique edit previews the exact candidate
- **WHEN** `edit_file` prepares a target whose old string occurs exactly once
- **THEN** its candidate applies that one replacement and, when within the computation budget, the complete diff describes the resulting whole-file change
- **THEN** the target remains byte-for-byte unchanged until commit

#### Scenario: Replace-all preview includes every replacement
- **WHEN** `edit_file` prepares a target with replace-all enabled and several matches
- **THEN** the candidate contains every replacement and, when within the computation budget, the complete diff accounts for every changed region

#### Scenario: File or candidate exceeds the safe preparation bound
- **WHEN** a target snapshot or derived candidate exceeds the fixed prepared-file byte limit
- **THEN** preparation fails with a typed size error before permission or mutation
- **THEN** a sparse or growing regular file cannot force an unbounded read allocation

#### Scenario: Invalid edit fails before permission and mutation
- **WHEN** the old string is missing, ambiguous without replace-all, or identical to the replacement
- **THEN** preparation returns the existing clear recoverable error
- **THEN** no permission approval is requested for a nonexistent candidate and the file remains unchanged

#### Scenario: Target changes after preparation
- **WHEN** a prepared write or edit reaches commit after its target existence or contents changed
- **THEN** the built-in returns a recoverable stale-action error and does not overwrite the newer state

#### Scenario: Content-identical target swap is not overwritten
- **WHEN** another process replaces the prepared target with a different file identity containing identical bytes
- **THEN** the identity-bound commit rejects the swap and does not write the replacement

#### Scenario: Symbolic-link target is refused
- **WHEN** preparation or commit observes a symbolic-link target or retargeted parent component
- **THEN** the built-in fails closed rather than following the link

#### Scenario: Hard-linked target is refused
- **WHEN** an existing write or edit target has more than one hard link at preparation or commit
- **THEN** the built-in returns typed `hard_link_alias_unsupported` or stale-action failure and modifies none of the aliases

#### Scenario: New target appears before exclusive create
- **WHEN** a create preview was approved and another process creates the target before commit
- **THEN** exclusive directory-relative creation fails without overwriting the new target

#### Scenario: Existing target metadata survives replacement
- **WHEN** a prepared existing file has mode, ownership, flags, ACL, or extended-attribute security metadata
- **THEN** commit clones and verifies that metadata on the complete staged candidate
- **THEN** successful atomic exchange changes file content without silently dropping those protections

#### Scenario: New target mode follows umask
- **WHEN** `write_file` creates a target under a restrictive process umask
- **THEN** its published mode matches direct-create umask semantics rather than unconditional `0644`

### Requirement: File diffs remain structured and bounded
Prepared file previews SHALL identify the normalized target path, create or
update operation, and before and candidate byte counts. Text-representable
previews SHALL also identify line and total changed-region counts and structured
added, removed, and context lines when the diff can be computed within the fixed
input and line budgets. The retained diff MUST fit the schema-v1
64 KiB or 800 logical-line preview bound. Bounding SHALL preserve operation and aggregate
metadata and attach a structured non-recoverable `preview_budget` omission; it MUST
NOT present a partial diff as complete. Non-text before state SHALL use an
explicit non-text replacement summary with byte-level identity metadata instead
of emitting a misleading line diff.

#### Scenario: In-bound diff exposes every changed region
- **WHEN** the complete prepared file diff fits the schema-v1 preview bound
- **THEN** all changed regions and their added and removed lines are retained with no preview omission

#### Scenario: Oversized diff preserves truthful totals
- **WHEN** the complete prepared file diff exceeds the preview bound
- **THEN** retained hunks stay within the bound and the preview remains renderable
- **THEN** total change counts still describe the complete candidate and a structured omission marks the retained diff as incomplete and non-recoverable from the stream

#### Scenario: Diff computation budget uses metadata fallback
- **WHEN** full before/candidate inputs exceed the fixed diff-computation byte or line budget
- **THEN** preparation does not split lines, construct operations, hunks, or render the full diff
- **THEN** the preview retains operation and exact before/candidate byte sizes, leaves uncomputed aggregate counts unknown, and reports a structured non-recoverable `preview_budget` omission

#### Scenario: New file without trailing newline remains truthful
- **WHEN** a new or replacement candidate lacks a trailing newline
- **THEN** the structured diff records that boundary state and does not invent an extra content line

#### Scenario: Non-text target uses an explicit fallback
- **WHEN** a write would replace existing content that cannot be represented safely as text lines
- **THEN** the preview identifies a non-text replacement with before and candidate byte counts and identity metadata
- **THEN** it does not claim to contain a complete line diff

