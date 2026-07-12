## ADDED Requirements

### Requirement: Stable delegated-agent provenance
Every accepted delegation SHALL receive a non-empty agent ID that is unique
within the root session and stable for that child's complete lifetime. Its typed
provenance MUST identify the parent session or parent agent ID, delegation depth,
human label, and originating task tool-call ID. Labels SHALL remain presentation
only and MUST NOT be used as identity, so repeated labels and nested work cannot
merge in a frontend.

#### Scenario: Root child receives stable identity
- **WHEN** the root accepts a valid task delegation
- **THEN** the child lifecycle identifies one new agent ID, the root session as parent, depth one, the supplied label, and the originating task call

#### Scenario: Nested child identifies its immediate parent
- **WHEN** a child delegates a grandchild within the depth limit
- **THEN** the grandchild receives its own agent ID, its delegating child as parent, and an incremented depth

#### Scenario: Duplicate labels remain separate
- **WHEN** two delegations use the same human-readable label
- **THEN** their agent IDs differ and lifecycle or permission events cannot be correlated by label alone

#### Scenario: One child identity spans permission and completion
- **WHEN** a child asks for permission and later finishes
- **THEN** both outward facts carry the same agent, parent, and depth provenance assigned at acceptance

### Requirement: Typed subagent lifecycle and terminal outcome
The observed root stream SHALL emit one started lifecycle fact for each child
that actually starts and exactly one terminal lifecycle fact for that child when
the stream remains deliverable. The terminal fact MUST use a typed outcome that
distinguishes completed, failed, cancelled, and reached-step-limit, and SHALL
carry the same stable provenance as start. It MUST precede completion of the
originating task tool call and the root terminal envelope. A rejected delegation
that constructs no child MUST NOT emit a false child-started fact.

#### Scenario: Successful child has one completed outcome
- **WHEN** a child starts and returns a valid final assistant answer
- **THEN** one start fact and one completed terminal fact carry its stable identity
- **THEN** the task tool result completes after the child terminal fact

#### Scenario: Recoverable child failure is typed
- **WHEN** a started child ends on provider failure, malformed completion, cleanup failure, or another recoverable child error
- **THEN** its terminal lifecycle outcome is failed and the task returns its existing recoverable error result to the parent

#### Scenario: Step limit is not mislabeled as failure
- **WHEN** a started child reaches its model-round limit
- **THEN** its terminal lifecycle outcome is reached-step-limit and the task returns the existing bounded step-limit error

#### Scenario: Parent cancellation identifies cancelled descendants
- **WHEN** parent cancellation stops one or more started descendants while the observed stream can still deliver terminal facts
- **THEN** each stopped descendant emits one cancelled terminal lifecycle fact before the root cancelled terminal envelope

#### Scenario: Pre-start rejection emits no false lifecycle
- **WHEN** validation, a before-tool block, permission denial, or depth limit prevents child construction
- **THEN** no child-started or child-terminal lifecycle fact is emitted for nonexistent work

### Requirement: Raw child activity remains isolated from the root stream
The orchestrator SHALL forward only typed subagent lifecycle facts and
child-origin permission interactions from a running child to the root observed
stream. It MUST NOT forward raw child assistant text, provider reasoning summary,
thinking or tool status, tool calls or results, hook outcomes, context usage,
warnings, omissions, errors, or child run-terminal envelopes. Successful child
answer content SHALL continue to cross the boundary only as the parent task
tool's final result, preserving the existing result-only contract.

#### Scenario: Child work does not create root transcript cards
- **WHEN** a child streams reasoning summaries and answer text and invokes several tools
- **THEN** the root observed stream contains no raw child reasoning, text, tool-start, tool-finish, or tool-output payload
- **THEN** the root may render only the child's lifecycle shell until the task result returns

#### Scenario: Child hooks and context stay private
- **WHEN** child lifecycle or tool hooks emit outcomes and child context usage changes
- **THEN** none of those hook, context, warning, or omission payloads is forwarded to the root stream

#### Scenario: Final answer crosses once as task result
- **WHEN** a child completes with a final assistant answer
- **THEN** that answer appears once as the originating task tool result
- **THEN** it is not also forwarded as child assistant text or a child run terminal payload

#### Scenario: Child permission is the deliberate exception
- **WHEN** a child tool requires a human decision
- **THEN** its permission request is forwarded with stable child provenance and its live one-reply path
- **THEN** answering it resumes that child without exposing the child's other raw events

#### Scenario: Nested lifecycle remains attributable
- **WHEN** a grandchild starts and ends
- **THEN** its typed lifecycle facts may reach the root with its own identity, immediate parent, and depth
- **THEN** the grandchild's raw work remains isolated by the same rules
