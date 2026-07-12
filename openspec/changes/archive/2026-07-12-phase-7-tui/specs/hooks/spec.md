## MODIFIED Requirements

### Requirement: Before-tool input shaping
A before-tool hook SHALL be able to allow, block, or replace tool arguments
before permission, sandbox, or tool execution occurs. Matching before-tool hooks
MUST run again whenever permission supplies edited or revised arguments, including
the legacy allow-with-edited-arguments path and the rich `revise_arguments` path.
Each hook replacement SHALL be revalidated before later hooks or stages consume
it, and a block, hook failure, or invalid replacement on any revision MUST fail
closed before preparation, permission approval of that revision, sandbox, or tool
execution. A successful rich revision SHALL be prepared and presented in a fresh
permission request before it can run.

#### Scenario: Before-tool block short-circuits downstream stages
- **WHEN** a before-tool hook blocks a tool call
- **THEN** permission is not consulted for that revision
- **THEN** preparation, sandbox, and the tool body do not run

#### Scenario: Before-tool replacement is revalidated
- **WHEN** a before-tool hook replaces a tool call's arguments
- **THEN** the replacement arguments are validated against the tool's declared shape before the call proceeds

#### Scenario: Permission-edited arguments cannot bypass a matching hook
- **WHEN** a legacy permission decision allows a call with edited arguments
- **THEN** matching before-tool hooks inspect those edited arguments before execution
- **THEN** a hook block or invalid hook replacement prevents sandbox and tool execution

#### Scenario: Legacy hook replacement invalidates the old approval
- **WHEN** rerun hooks validly replace human-edited arguments from a legacy approval
- **THEN** the replacement is revalidated and prepared but does not execute under the old approval
- **THEN** a fresh legacy permission request presents the replacement before any sandbox or tool execution

#### Scenario: Rich revision is rechecked and reapproved
- **WHEN** a rich permission request receives revised arguments
- **THEN** matching before-tool hooks run against the revised effective call
- **THEN** a valid allowed hook result is revalidated and prepared
- **THEN** the harness issues a fresh permission request for that revision before execution

#### Scenario: Hook replacement composition remains deterministic on revision
- **WHEN** several matching before-tool hooks replace a permission revision
- **THEN** replacements compose in configured hook order with validation between replacements
- **THEN** the final valid replacement is the one prepared for the next permission request
