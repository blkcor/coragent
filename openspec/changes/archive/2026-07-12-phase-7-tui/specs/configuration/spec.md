## ADDED Requirements

### Requirement: Public settings discovery for first-party clients
The public SDK SHALL expose a `LoadSettings` operation that gives first-party
clients the existing home-and-project settings discovery, defaults, validation,
environment resolution, and field-level merge behavior without importing the
internal configuration package. The public operation MUST use the canonical
loader rather than implementing a second parser or merge algorithm.

#### Scenario: Binary loads home and project settings through the SDK
- **WHEN** `cmd/coragent` starts in a project with home and project settings
- **THEN** it can call public `LoadSettings` through `pkg/agent`
- **THEN** project fields override overlapping home fields and non-overlapping home fields are preserved exactly as for the existing loader

#### Scenario: No settings files exist
- **WHEN** a public client calls `LoadSettings` and neither settings file exists
- **THEN** loading succeeds with the existing documented defaults

#### Scenario: Public loading encounters malformed settings
- **WHEN** a discovered settings file contains malformed JSON or an invalid hook, permission, model, or sandbox field
- **THEN** `LoadSettings` fails with an error that names the offending file and safe field context
- **THEN** no partially configured session is returned

#### Scenario: Environment credential reference is unset
- **WHEN** settings reference an unset credential environment variable
- **THEN** public loading preserves the existing missing-credential behavior
- **THEN** a later provider request or validating bootstrap fails with a clear secret-free error according to the selected bootstrap policy

### Requirement: Validated public session bootstrap
The public SDK SHALL provide a bootstrap operation that accepts the result of
public settings loading and constructs the standard provider, hooks, permission
configuration, sandbox policy, built-in tools, and session through the existing
`Session` construction path. Bootstrap SHALL return the session and safe
frontend-neutral descriptor facts needed by `cmd/coragent`; it MUST NOT create a
TUI-specific session type or bypass the single execution chokepoint.

#### Scenario: First-party binary bootstraps a default session
- **WHEN** `cmd/coragent` supplies valid loaded settings and its working directory to the public bootstrap operation
- **THEN** bootstrap constructs a usable public session with the configured model, hooks, permission mode and rules, sandbox grants, and built-in tools
- **THEN** every tool call still flows through hooks, permission, sandbox routing when applicable, and the tool handler in the established order

#### Scenario: Bootstrap validates before interactive startup
- **WHEN** settings cannot construct the provider, hooks, permission engine, sandbox, or session
- **THEN** bootstrap returns one actionable error before the frontend accepts a user request
- **THEN** no half-started provider stream, hook process, or session remains active

#### Scenario: Bootstrap supplies safe descriptor facts
- **WHEN** bootstrap succeeds
- **THEN** the caller can obtain effective model identity, starting mode, sandbox posture, truthful known-or-unknown context-window state, and effective capability metadata through public frontend-neutral values
- **THEN** the descriptor does not expose resolved credentials or secret-bearing configuration values

#### Scenario: SDK embedder uses explicit construction
- **WHEN** an SDK embedder already constructs a provider and calls `NewSession` or `NewSessionWithError` directly
- **THEN** that path remains supported without calling public settings discovery or bootstrap
- **THEN** its supplied configuration, including an explicit `SessionConfig.SystemPrompt`, remains authoritative

### Requirement: First-party Coragent product framing
The public `Bootstrap` operation used by first-party clients SHALL construct its
session with a non-empty system framing that identifies the assistant and product
as Coragent. The framing MUST distinguish that product identity from the
configured provider or model, which remains a replaceable backend, and MUST NOT
instruct the assistant to adopt a backend vendor's branded assistant identity.
This first-party default SHALL NOT prefix, replace, or reinterpret an explicit
`SessionConfig.SystemPrompt` supplied by an SDK embedder through direct session
construction. The framing MUST remain inside the conversation boundary and MUST
NOT be exposed through public descriptors, safe settings views, logs, or errors.

#### Scenario: First-party user asks who the assistant is
- **WHEN** `cmd/coragent` creates a session through public `Bootstrap` and the user asks for the assistant's identity
- **THEN** the provider request contains non-empty framing that identifies the product as Coragent
- **THEN** the framing treats the configured model or provider as a replaceable backend rather than the assistant's product identity

#### Scenario: Backend has a competing pretrained persona
- **WHEN** a configured backend would otherwise describe itself using a vendor assistant name
- **THEN** the first-party system framing instructs it to answer as Coragent and to separate any truthful backend detail from product identity
- **THEN** the frontend does not post-process or rewrite the completed assistant answer to simulate that identity

#### Scenario: Explicit SDK system prompt remains authoritative
- **WHEN** an SDK embedder passes a non-empty `SessionConfig.SystemPrompt` to `NewSession` or `NewSessionWithError`
- **THEN** that exact caller-owned framing seeds the conversation without an implicit Coragent prefix or replacement
- **THEN** using the public first-party bootstrap elsewhere does not change the embedder's session behavior

#### Scenario: Product framing stays out of public diagnostics
- **WHEN** bootstrap succeeds or fails and public values are described, formatted, logged, or serialized
- **THEN** no system-prompt content is included in those values
- **THEN** safe product and backend labels may remain available as separate non-secret facts

### Requirement: Secret-safe public configuration boundary
Public configuration SHALL keep resolved credentials only in the provider or opaque bootstrap
state that needs them. Public settings diagnostics, descriptors, errors, string
representations, JSON representations, logs, and frontend capability metadata
MUST redact or omit credential values and MUST NOT expose environment contents or
secret-bearing hook command arguments.

#### Scenario: Credential resolves successfully
- **WHEN** `LoadSettings` resolves an API key from an environment variable and bootstrap constructs a provider
- **THEN** the provider can authenticate with the resolved value
- **THEN** the resolved value is absent from public descriptors, rendered diagnostics, logs, and serializable safe settings views

#### Scenario: Provider construction fails near a credential
- **WHEN** provider validation fails while processing credential configuration
- **THEN** the error may name the safe setting field or environment variable name
- **THEN** the error does not include the credential value

#### Scenario: Hook metadata is exposed to a frontend
- **WHEN** a public descriptor reports configured hooks
- **THEN** it exposes only safe hook identity, lifecycle moment, source, and availability needed for inspection
- **THEN** it omits environment values and command arguments that may contain secrets

#### Scenario: Public value is formatted or serialized
- **WHEN** a loaded-settings, bootstrap-result, or descriptor value is formatted, logged, or encoded through a supported representation
- **THEN** credential fields remain redacted or absent by construction

### Requirement: Frontend presentation remains outside harness settings
The harness settings schema SHALL remain frontend-neutral. Phase 7 MUST NOT add
TUI layout, color, animation, reduced-motion, Unicode, keybinding, viewport, or
transcript-folding fields to the shared `settings.json`; such presentation
choices SHALL be owned by the TUI entry point through local options, terminal
capability detection, or frontend-specific environment variables.

#### Scenario: Existing settings file starts the TUI
- **WHEN** a user launches the TUI with an existing valid harness settings file
- **THEN** no new TUI section is required for startup
- **THEN** existing model, hook, permission, and sandbox settings retain their meanings

#### Scenario: Frontend selects an accessibility fallback
- **WHEN** the TUI enables no-color, reduced-motion, or ASCII presentation
- **THEN** the choice affects only frontend rendering
- **THEN** it does not mutate session policy or add a field to harness settings

#### Scenario: SDK client is not a TUI
- **WHEN** a different frontend loads the same settings through the public SDK
- **THEN** it receives harness configuration without Bubble Tea, terminal-layout, or TUI keybinding concepts

### Requirement: Legacy loading behavior remains compatible
Adding public loading and bootstrap SHALL preserve the existing `Load`,
`LoadFrom`, defaults, environment-reference, merge, validation, missing-file, and
remembered-permission persistence behavior used by prior phases. Existing public
`SessionConfig`, `NewSession`, and `NewSessionWithError` callers MUST remain
source- and behavior-compatible.

#### Scenario: Legacy Load discovers files
- **WHEN** an existing internal caller invokes the legacy `Load` operation with the same home directory, project directory, environment, and files as before Phase 7
- **THEN** it receives the same merged settings or equivalent error as before Phase 7

#### Scenario: Legacy LoadFrom skips discovery
- **WHEN** an existing caller supplies settings directly through `LoadFrom`
- **THEN** no home or project discovery occurs
- **THEN** the same defaults and supplied overrides apply as before Phase 7

#### Scenario: Remembered permission rule is persisted
- **WHEN** a prior-phase session persists a remembered allow or deny rule
- **THEN** the existing project settings location and read-modify-write preservation behavior remain unchanged

#### Scenario: Existing SDK caller compiles unchanged
- **WHEN** a client written against the pre-Phase-7 `SessionConfig`, `NewSession`, or `NewSessionWithError` API is rebuilt
- **THEN** it requires no source change
- **THEN** it is not forced through file discovery or first-party bootstrap
