# action-preview Specification

## Purpose
TBD - created by archiving change phase-7-tui. Update Purpose after archive.
## Requirements
### Requirement: Side-effect-free action preparation
The harness SHALL support a frontend-neutral preparation step for an action whose
handler can preview its effects. The additive `PreparedActionHandler` contract
SHALL remain the identity-bound prepare/commit seam for mutations. A separate
additive `ActionPreviewer` contract SHALL describe actions that do not require a
commit token. Both seams MUST receive validated effective arguments and be
cancellable and side-effect-free during preview. A previewer MUST NOT execute the
tool, launch a command, walk or read a target merely to produce declarative
metadata, or start a delegated child. A preparation or preview failure SHALL stop
the call before permission can approve a misdescribed action or execution can
mutate state.

#### Scenario: Preparing a file action leaves the filesystem unchanged
- **WHEN** a preview-capable file action is prepared
- **THEN** the harness may read the target and its parent metadata
- **THEN** no file or directory is created, changed, or removed by preparation

#### Scenario: Preparation failure stops the action
- **WHEN** the harness cannot read or prepare the state needed for a truthful preview
- **THEN** it returns a recoverable error correlated to the tool call
- **THEN** permission is not asked to approve a fabricated preview and no mutation occurs

#### Scenario: Cancellation interrupts preparation
- **WHEN** the governing context is cancelled while an action is being prepared
- **THEN** preparation stops promptly and no prepared action is committed

#### Scenario: Preview-only seam does not become a commit path

- **WHEN** a handler implements `ActionPreviewer` without `PreparedActionHandler`
- **THEN** the executor obtains its preview before permission and later invokes the ordinary execution path only after approval
- **THEN** previewing alone does not execute the action

### Requirement: Effective arguments govern every prepared revision
The first prepared revision SHALL use the validated arguments remaining after
before-tool hooks. A rich permission argument revision MUST NOT approve the
action: the harness SHALL validate the reply and revised arguments without
consuming the request on validation failure. After accepting a schema-valid
revision, it SHALL close that request and run the applicable hard before-tool
checks for that revision. Only when those checks allow and preparation succeeds
SHALL it create a replacement revision and permission request. Only the latest approved revision SHALL be eligible
to execute, and an older revision or preview MUST NOT be reused after arguments
change.

The same revision rules SHALL apply to `ActionPreviewer`: a hook edit or accepted
permission argument revision MUST recompute the preview from the new effective
arguments, increment the revision, and MUST NOT reuse an older preview.

#### Scenario: Hook-edited arguments are previewed
- **WHEN** a before-tool hook replaces valid action arguments
- **THEN** the prepared action and preview describe the hook-edited arguments rather than the provider's original arguments

#### Scenario: Permission revision replaces the preview
- **WHEN** the rich permission protocol accepts a schema-valid `revise_arguments` reply, hard hooks allow it, and preparation succeeds
- **THEN** that reply does not approve execution
- **THEN** the revised arguments are revalidated, hard-checked, and used to create a new prepared revision and permission request

#### Scenario: Preview-only handler recomputes a revision

- **WHEN** the effective arguments of an `ActionPreviewer` change after a hook or permission revision
- **THEN** the harness invokes the previewer again and correlates the replacement preview to the incremented revision

#### Scenario: Accepted revision is blocked before replacement preparation
- **WHEN** a schema-valid revision resolves its request and a hard hook blocks, fails, or produces an invalid replacement
- **THEN** the call terminates without creating a replacement prepared revision or permission request
- **THEN** no sandbox or tool mutation begins

#### Scenario: Stale revision cannot execute
- **WHEN** revision two exists for a call and an allow reply targets revision one
- **THEN** the harness ignores the stale approval and does not execute either revision from it

### Requirement: Structured previews describe the candidate effect
Every preview-capable action SHALL carry a structured preview correlated to its tool
call and revision. The preview MUST identify its action kind and affected targets,
and SHALL describe the exact candidate effect using typed fields such as a file
diff or command summary rather than requiring a frontend to parse prose. When a
handler implements neither preview seam, the harness SHALL report typed unavailability
with a reason and MUST NOT invent one from a tool name or result string.

The first-party `read_file`, `search_content`, `find_files`, `run_command`, and
`task` handlers SHALL implement `ActionPreviewer`. Their previews SHALL resolve
the same defaults execution uses and SHALL truthfully describe respectively the
read window; search pattern, path, glob, and case mode; file glob, root, and
skipped directories; full command and effective timeout; and task label, bounded
instruction summary, and effective child tool set. `write_file` and `edit_file`
SHALL retain their identity-bound prepared file previews.

#### Scenario: Text file mutation provides a typed diff
- **WHEN** a preview-capable text file mutation is prepared
- **THEN** its preview identifies the target, operation, and structured before-to-after diff
- **THEN** a frontend can render the diff without parsing a human-readable confirmation

#### Scenario: Preview support is unavailable
- **WHEN** an existing custom handler implements neither `PreparedActionHandler` nor `ActionPreviewer`
- **THEN** the action retains its existing execution behavior
- **THEN** the preview state is explicitly unavailable rather than fabricated

#### Scenario: First-party tools provide authoritative previews

- **WHEN** read, search, find, command, or task reaches the preview step
- **THEN** its structured preview contains the effective operation details and is not typed unavailable
- **THEN** generating that preview performs no read, search, walk, command launch, or child startup

#### Scenario: Preview identity follows one call revision
- **WHEN** a prepared preview is delivered through an observed event or rich permission request
- **THEN** it carries the same tool-call correlation ID and revision as the effective action it describes

### Requirement: Preview payloads are bounded without hiding loss
The harness SHALL bound preview payloads independently of tool-result output. It
MUST project from an empty result into bounded storage and MUST NOT clone an
unbounded handler preview before applying the bound. One shared maximum of 64 KiB
of valid UTF-8 or 800 logical preview lines, whichever is reached first, SHALL
cover summary, targets, unavailable reason, metadata keys and values, text,
file-diff path, hunks and lines, omission identifiers, and every projected
collection entry. If a complete preview exceeds that bound, projection MUST
preserve typed scalar aggregates such as operation and file-diff counts, retain
only content that fits on valid text boundaries, and attach a structured
`preview_budget` omission containing known original and retained sizes and
non-recoverable status. Frontend folding of a fully retained preview SHALL remain
a reversible presentation choice and MUST NOT be reported as harness truncation.
Frontend terminal wrapping MUST NOT change the harness bound.

#### Scenario: Complete preview fits the bound
- **WHEN** a prepared diff fits within the preview bound
- **THEN** every changed hunk is retained and no action-preview omission is reported

#### Scenario: Oversized preview is explicitly incomplete
- **WHEN** a prepared diff exceeds the preview bound
- **THEN** the retained preview stays within the bound and remains valid text
- **THEN** aggregate change counts remain truthful and a structured `preview_budget` omission states that omitted preview content cannot be recovered from the stream

#### Scenario: Oversized collections cannot bypass the budget
- **WHEN** an action preview contains oversized targets, metadata, hunks, or diff lines
- **THEN** every retained string and collection entry participates in the same byte and logical-line budget
- **THEN** the projected collection capacities remain bounded and typed aggregate counts remain available

#### Scenario: Both preview contracts bound before cloning
- **WHEN** either an `ActionPreviewer` or `PreparedActionHandler` returns an oversized preview
- **THEN** the executor applies bounded projection before making any independent preview clone
- **THEN** a prepared handler's opaque commit token and candidate identity remain unchanged

#### Scenario: Terminal width does not change the preview budget
- **WHEN** the same prepared preview is observed by frontends with different wrap widths
- **THEN** its retained UTF-8 body, logical-line count, aggregate metadata, and omission facts remain identical

#### Scenario: UI fold does not become an omission
- **WHEN** a frontend collapses a fully retained preview card
- **THEN** the harness emits no omission for that fold
- **THEN** expanding the card can reveal all retained preview content

### Requirement: Approved preview and committed mutation stay consistent
Execution of a prepared mutating action SHALL commit the exact candidate state
described by the latest approved preview. Preparation MUST capture a target-state
precondition sufficient to detect a change between preparation and commit,
including target type, no-follow symlink state, parent identity, stable target
identity when present, hard-link count, and preimage content. An existing target
whose link count is not exactly one MUST fail preparation as typed
`hard_link_alias_unsupported`, because every alias cannot be enumerated as an
affected target. Commit MUST reacquire and validate
the target using no-follow, directory-relative semantics and MUST mutate through
the verified directory-relative path only after writing and syncing a complete
same-directory temporary candidate, revalidating identity and preimage, and
atomically exchanging the directory entry. An existing target's ownership,
mode, flags, file-specific ACLs, and extended attributes MUST be preserved by a
verified operating-system metadata clone; if they cannot be preserved exactly,
commit MUST fail closed. New targets MUST retain ordinary process-umask creation
semantics and publish through an atomic exclusive rename. Symlink targets MUST be refused. A
failed precondition SHALL abort with a recoverable stale-action error instead of
recomputing or applying an unreviewed mutation. Preparing or observing an action
MUST NOT itself grant execution authority or bypass hooks, permission, sandbox,
or cancellation. If the platform or filesystem cannot provide the required
identity, metadata-clone, and atomic-replacement primitives, the action MUST fail closed
without mutation.

#### Scenario: Unchanged target commits the reviewed candidate
- **WHEN** the target still satisfies the prepared precondition after the latest revision is approved
- **THEN** execution applies exactly the candidate state represented by that preview

#### Scenario: Concurrent target change fails closed
- **WHEN** the target changes after preview preparation but before commit
- **THEN** the action returns a recoverable stale-action error
- **THEN** the harness does not overwrite the newer state or silently generate and apply a different candidate

#### Scenario: Same-byte identity replacement fails closed
- **WHEN** the prepared path is replaced by a different file identity with identical bytes before commit
- **THEN** identity validation fails and the replacement file is not written

#### Scenario: Symlink or parent path is retargeted
- **WHEN** a target becomes a symlink, a symlink is retargeted, or the verified parent identity changes after preparation
- **THEN** no-follow identity validation fails without writing through the changed path

#### Scenario: Existing target has a hard-link alias
- **WHEN** preparation observes an existing regular file whose link count is greater than one
- **THEN** it fails closed as `hard_link_alias_unsupported` before permission and does not claim that the displayed path is the only affected target

#### Scenario: Hard link appears before commit
- **WHEN** a prepared single-link target has a greater link count when the verified commit handle is checked
- **THEN** commit returns a recoverable stale-action error and writes no alias

#### Scenario: Commit publishes a complete candidate atomically
- **WHEN** target identity and content still match and the platform supports the required primitives
- **THEN** commit writes and syncs a same-directory temporary candidate before its final identity and preimage validation
- **THEN** cancellation or a write failure before replacement leaves the original target byte-for-byte intact and cleans up the temporary candidate
- **THEN** the final directory-entry replacement is atomic, so readers observe either the complete prior file or the complete candidate

#### Scenario: Existing security metadata is preserved
- **WHEN** an existing target carries ownership, mode, user flags, file-specific ACLs, or extended attributes
- **THEN** the staged candidate inherits and verifies those security properties before atomic exchange
- **THEN** inability to copy the metadata exactly fails closed before the original entry is replaced

#### Scenario: New file respects the process umask
- **WHEN** a create action publishes a new target while the process umask is restrictive
- **THEN** the final mode equals the requested create mode after ordinary umask application
- **THEN** staging never widens the result with a post-create chmod that bypasses the umask

#### Scenario: Final path race is rolled back
- **WHEN** another regular-file identity replaces the target after final validation but before atomic exchange
- **THEN** the exchanged-out identity is checked against the prepared target
- **THEN** a mismatch is atomically restored and the candidate is discarded as stale rather than deleting or overwriting the other writer's file

#### Scenario: Safe identity primitive is unavailable
- **WHEN** the platform or filesystem cannot provide stable identity, reliable link count, no-follow validation, security-metadata cloning, and atomic directory-relative exchange
- **THEN** the prepared mutation fails closed as unsupported and no best-effort path write occurs

#### Scenario: Preview does not widen authority
- **WHEN** an action can be prepared and rendered
- **THEN** it still passes every applicable hook, permission, and sandbox rule before execution
