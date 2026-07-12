## ADDED Requirements

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
