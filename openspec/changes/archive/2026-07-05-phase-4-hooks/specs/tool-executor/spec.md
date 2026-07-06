## MODIFIED Requirements

### Requirement: Single ordered execution chain

The executor SHALL route every tool call — built-in or custom — through exactly
one ordered chain of stages: before-tool hard hooks → human permission → sandbox
→ execute → after-tool hard hooks. No tool call SHALL reach the user's machine by
any other route. The executor SHALL fulfil the existing `Dispatcher` seam without
changing its signature.

#### Scenario: Every capability travels the one path

- **WHEN** a read, write, edit, content-search, file-find, shell, or custom tool call is dispatched
- **THEN** it passes through the same ordered chain and none has a private bypass route

#### Scenario: Fixed order is observable

- **WHEN** a shell call is dispatched through an instrumented chain
- **THEN** the stages run in exactly this order — before-tool hard hooks → human permission → sandbox → execute → after-tool hard hooks — and the order is asserted in tests, not merely intended

### Requirement: Inert placeholder stages

The permission and sandbox stages SHALL remain drop-in stages supplied by their
own phases, while the hard pre-check and hard post-check stage slots SHALL be
filled by the hooks capability without altering the executor path. A session with
no matching hooks SHALL pass through the hard stages exactly as the Phase 2 inert
placeholders did.

#### Scenario: No configured hooks preserves pass-through behavior

- **WHEN** a read, edit, or shell call runs through the chain with no matching hooks configured
- **THEN** the hard hook stages allow the call and the result is identical to what the tool would produce on its own

### Requirement: Hard pre-check short-circuit

When a before-tool hard hook blocks a call, the executor SHALL prevent
permission, the sandbox, and the tool's work from running, and SHALL return an
error result carrying the block's reason. There SHALL be no path for the model or
permission bypass mode to override a hard block.

#### Scenario: Pre-check block stops all downstream work

- **WHEN** a before-tool hard hook blocks a call
- **THEN** permission, sandbox, and the tool never run, and the result is an error carrying the block's reason

#### Scenario: Permission bypass does not affect hard pre-check

- **WHEN** permission is configured to bypass human prompts
- **WHEN** a before-tool hard hook blocks a call
- **THEN** the call is still blocked before permission, sandbox, or tool execution

### Requirement: Hard post-check veto

The executor SHALL apply after-tool hard hook verdicts before handing a tool
result back to the model. A blocking verdict SHALL turn the result into an error
carrying the block's reason, and a replacement verdict SHALL replace the result
content the model receives.

#### Scenario: Post-check turns success into a blocked error

- **WHEN** an after-tool hard hook blocks an otherwise-successful result
- **THEN** the result handed back to the model becomes an error carrying the block's reason

#### Scenario: Post-check replacement reaches the model

- **WHEN** an after-tool hard hook replaces an otherwise-successful result
- **THEN** the result handed back to the model contains the replacement content rather than the original content

## ADDED Requirements

### Requirement: Hook-edited arguments are revalidated
When a before-tool hard hook returns edited arguments, the executor SHALL
revalidate those arguments against the tool's declared shape before consulting
permission or running the tool.

#### Scenario: Invalid hook edit blocks the call
- **WHEN** a before-tool hook returns edited arguments that do not fit the tool's declared shape
- **THEN** the call is blocked with a validation error result
- **THEN** permission, sandbox, and the tool do not run

#### Scenario: Valid hook edit proceeds
- **WHEN** a before-tool hook returns edited arguments that fit the tool's declared shape
- **THEN** the edited arguments are the arguments passed to later stages
