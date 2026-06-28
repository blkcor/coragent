## ADDED Requirements

### Requirement: Architecture-prescribed directory structure exists
The system SHALL provide every directory the architecture prescribes in a fresh checkout, including `pkg/agent/` (public SDK surface), `internal/` (harness internals with subdirectories for loop, context, executor, permission, hooks, sandbox, provider, tools, subagent), `tui/`, `cmd/coragent/`, and `docs/`.

#### Scenario: Fresh checkout contains all prescribed directories
- **WHEN** a developer clones the repository
- **THEN** all directories specified in the architecture exist

#### Scenario: Placeholder directories for unlanded phases build
- **WHEN** a phase has not yet been implemented
- **THEN** its directory contains a minimal `doc.go` file with a package comment
- **THEN** the directory builds successfully as a placeholder

### Requirement: Public surface depends on no internal machinery
The system SHALL keep the public surface (`pkg/agent/`) free of any dependency on internal packages (`internal/`) or any frontend (`tui/`, `cmd/`).

#### Scenario: Public package imports only standard library and external dependencies
- **WHEN** the public package is compiled
- **THEN** it imports no packages from `internal/`
- **THEN** it imports no packages from `tui/` or `cmd/`

#### Scenario: Automated verification of import independence
- **WHEN** the build runs
- **THEN** a linter or build check verifies that `pkg/agent/` does not import `internal/`, `tui/`, or `cmd/`

### Requirement: Internal packages are not importable outside the module
The system SHALL place all harness internals under `internal/` so that Go's compiler prevents external modules from importing them.

#### Scenario: External module cannot import internal packages
- **WHEN** an external Go module attempts to import a package from `coragent/internal/`
- **THEN** the Go compiler rejects the import with an error

### Requirement: Build, typecheck, and unit tests pass from fresh checkout
The system SHALL build, pass static checks, and pass unit tests from a fresh checkout with no edits required.

#### Scenario: Fresh checkout builds successfully
- **WHEN** a developer runs `go build ./...` in a fresh checkout
- **THEN** the build succeeds with no errors

#### Scenario: Fresh checkout passes static checks
- **WHEN** a developer runs `golangci-lint run ./...` in a fresh checkout
- **THEN** the linter reports no errors

#### Scenario: Fresh checkout passes unit tests
- **WHEN** a developer runs `go test ./...` in a fresh checkout
- **THEN** all tests pass

### Requirement: Shared vocabulary for core concepts
The system SHALL expose one shared, documented vocabulary for the conversation, turn, tool, tool call, tool result, model backend, and run events on the public surface, named in user-facing terms with no invented abbreviations.

#### Scenario: Core concept types are present on public surface
- **WHEN** a developer imports `pkg/agent/`
- **THEN** the following types are available: Conversation, Turn, Tool, ToolCall, ToolResult, Provider, RunEvent

#### Scenario: Core concepts are documented with user-facing names
- **WHEN** a developer reads the package documentation
- **THEN** each core concept is documented with its purpose and usage
- **THEN** no invented abbreviations are used in type names or documentation

### Requirement: Permission request and decision shape is declared
The system SHALL include the permission request and decision shape in the shared vocabulary, carrying allow/deny, optional "remember this", and optional edited arguments, even though no Phase 0 component emits or acts on it.

#### Scenario: Permission request type exists with reply path
- **WHEN** a developer inspects the public vocabulary
- **THEN** a PermissionRequest type exists
- **THEN** it carries a way to send a decision back (allow or deny)
- **THEN** it supports optional "remember this" flag
- **THEN** it supports optional edited arguments

#### Scenario: Permission decision type exists
- **WHEN** a developer inspects the public vocabulary
- **THEN** a PermissionDecision type exists
- **THEN** it expresses allow or deny
- **THEN** it expresses optional "remember this"
- **THEN** it expresses optional edited arguments

#### Scenario: Permission shape is documented but not acted on
- **WHEN** a developer reads the permission type documentation
- **THEN** the documentation states that Phase 0 declares the shape but does not emit or act on it
- **THEN** the documentation references the phase that will implement it
