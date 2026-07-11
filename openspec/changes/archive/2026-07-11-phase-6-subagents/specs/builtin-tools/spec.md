## ADDED Requirements

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
