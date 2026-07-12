# Configuration

## Purpose

Configuration loading and merging for the Coragent harness — single JSON settings file with home/project discovery, field-level merge, environment-based credentials, and direct in-code override.
## Requirements
### Requirement: Single settings file format
The system SHALL configure the harness from a single JSON settings file with a documented format, available fields, and defaults.

#### Scenario: Settings file is JSON with documented fields
- **WHEN** a developer creates a settings file
- **THEN** the file is in JSON format
- **THEN** available fields are documented with their types and defaults

#### Scenario: Settings file configures model backend
- **WHEN** a settings file specifies model backend options
- **THEN** those options are applied to the model backend configuration

### Requirement: Home-and-project discovery with field-level merge
The system SHALL discover settings in both the home directory (`~/.coragent/settings.json`) and the current project (`.coragent/settings.json`), merging them field-by-field with the project value taking precedence per overlapping field.

#### Scenario: Settings exist only in home directory
- **WHEN** settings exist at `~/.coragent/settings.json`
- **WHEN** no settings exist in the project
- **THEN** the home settings apply

#### Scenario: Settings exist only in project directory
- **WHEN** settings exist at `.coragent/settings.json`
- **WHEN** no settings exist in home
- **THEN** the project settings apply

#### Scenario: Settings exist in both locations with overlapping fields
- **WHEN** settings exist at both `~/.coragent/settings.json` and `.coragent/settings.json`
- **WHEN** both files define the same field
- **THEN** the project's value wins for that field
- **THEN** non-overlapping home fields are preserved

### Requirement: Missing settings file is harmless
The system SHALL treat a missing settings file as harmless, falling back to the other location or to documented defaults.

#### Scenario: No settings file exists in either location
- **WHEN** neither `~/.coragent/settings.json` nor `.coragent/settings.json` exists
- **THEN** loading succeeds
- **THEN** documented defaults apply

#### Scenario: Settings file missing in one location
- **WHEN** settings exist in one location but not the other
- **THEN** loading succeeds
- **THEN** the existing file's settings apply

### Requirement: Malformed settings file fails loudly
The system SHALL fail loudly on a malformed settings file and name the offending file in the error message.

#### Scenario: Settings file contains invalid JSON
- **WHEN** a settings file exists but contains invalid JSON
- **THEN** loading fails with an error
- **THEN** the error message names the offending file path

#### Scenario: Settings file contains invalid field values
- **WHEN** a settings file exists with valid JSON but invalid field values
- **THEN** loading fails with an error
- **THEN** the error message names the offending file path
- **THEN** the error message describes the invalid field

### Requirement: Credentials drawn from environment
The system SHALL resolve model credentials from environment variables rather than literal values in the settings file.

#### Scenario: Settings file references environment variable
- **WHEN** a settings file contains `"api_key": "${OPENAI_API_KEY}"`
- **WHEN** the environment variable `OPENAI_API_KEY` is set
- **THEN** the credential value is resolved from the environment at load time

#### Scenario: Environment variable is unset
- **WHEN** a settings file references an environment variable
- **WHEN** that environment variable is not set
- **THEN** the credential is left empty
- **THEN** the first API request fails loudly with a clear error

#### Scenario: No literal credentials in settings file
- **WHEN** a developer commits a project settings file
- **THEN** no credential value is stored literally in the file

### Requirement: Direct in-code configuration
The system SHALL accept configuration supplied directly in code and perform no file discovery in that case.

#### Scenario: Configuration supplied in code skips file discovery
- **WHEN** an SDK embedder supplies configuration as a Go struct
- **THEN** no file discovery happens
- **THEN** the supplied configuration is honored as given

#### Scenario: In-code configuration bypasses merge logic
- **WHEN** configuration is supplied directly in code
- **THEN** no home/project merge happens
- **THEN** the supplied configuration is used exactly as provided

### Requirement: External hook settings
The settings file SHALL allow operators to declare external command hooks with a
name, lifecycle moment, command, optional scope, and timeout.

#### Scenario: Settings declare external hook
- **WHEN** a settings file declares an external hook with a valid moment, command, scope, and timeout
- **THEN** session construction makes that hook available at the configured moment

#### Scenario: Settings declare run-finished hook
- **WHEN** a settings file declares an external hook with moment `run-finished`
- **THEN** configuration validation accepts the hook
- **THEN** session construction makes that hook available when runs finish

#### Scenario: Hook settings remain JSON
- **WHEN** a developer creates hook settings
- **THEN** the settings remain part of the single JSON settings file format

### Requirement: Hook settings merge with project override
Home and project settings SHALL merge hook configuration according to the same
field-level merge rules as the rest of configuration, with project settings
taking precedence for overlapping hook fields.

#### Scenario: Home hook settings apply
- **WHEN** hook settings exist only at `~/.coragent/settings.json`
- **THEN** those hook settings apply

#### Scenario: Project hook settings override home hook settings
- **WHEN** hook settings exist in both home and project settings
- **THEN** overlapping project hook settings take precedence
- **THEN** non-overlapping home settings are preserved according to the documented merge rules

### Requirement: Hook definition validation
Malformed hook definitions SHALL fail session construction or configuration
validation loudly with an error naming the offending hook and field.

#### Scenario: Invalid hook moment fails loudly
- **WHEN** a hook definition names an invalid lifecycle moment
- **THEN** validation fails with an error naming the offending hook and moment field

#### Scenario: Invalid hook pattern fails loudly
- **WHEN** a hook definition contains an invalid pattern scope
- **THEN** validation fails with an error naming the offending hook and pattern field

#### Scenario: Invalid hook timeout fails loudly
- **WHEN** a hook definition contains an invalid timeout
- **THEN** validation fails with an error naming the offending hook and timeout field

### Requirement: Permission settings section

The settings file SHALL support a permission section configuring the starting mode
and the allow and deny rule lists, alongside the existing model settings. The
permission section SHALL be merged field-by-field home-then-project with the rest
of the settings, project taking precedence.

#### Scenario: Settings file configures permission mode and rules

- **WHEN** a settings file specifies a permission starting mode and allow/deny rules
- **THEN** those values are applied to the permission configuration

#### Scenario: Permission section absent uses documented defaults

- **WHEN** a settings file omits the permission section
- **THEN** the documented default mode and empty rule lists apply

#### Scenario: Permission rules merge home-then-project

- **WHEN** both home and project settings define permission rules
- **THEN** both layers apply with the project's rules taking precedence per the documented merge

### Requirement: Sandbox settings
The settings file SHALL allow operators to configure sandbox policy grants using
the single JSON settings format. Sandbox settings SHALL support extra readable
roots, extra writable roots, and explicit network access.

#### Scenario: Settings declare extra sandbox roots
- **WHEN** a settings file declares sandbox extra read roots and extra write roots
- **THEN** configuration validation accepts valid paths
- **THEN** policy derivation receives those roots as additive grants

#### Scenario: Settings declare network access
- **WHEN** a settings file explicitly grants sandbox network access
- **THEN** policy derivation receives network access as an additive grant

#### Scenario: Missing sandbox settings use safe defaults
- **WHEN** no sandbox settings are present
- **THEN** loading succeeds
- **THEN** sandbox policy derivation uses the safe baseline with network denied

### Requirement: Sandbox settings merge with project override
Home and project settings SHALL merge sandbox configuration according to the same
field-level merge rules as the rest of configuration, with project settings
taking precedence for overlapping sandbox fields.

#### Scenario: Home sandbox settings apply
- **WHEN** sandbox settings exist only at `~/.coragent/settings.json`
- **THEN** those sandbox settings apply

#### Scenario: Project sandbox settings override home sandbox settings
- **WHEN** sandbox settings exist in both home and project settings
- **THEN** overlapping project sandbox settings take precedence
- **THEN** non-overlapping home sandbox settings are preserved according to the documented merge rules

### Requirement: Sandbox settings validation
Malformed sandbox settings SHALL fail configuration validation loudly with an
error naming the offending file and field.

#### Scenario: Invalid sandbox path fails loudly
- **WHEN** a settings file contains an invalid sandbox read or write root
- **THEN** loading fails with an error naming the offending file path
- **THEN** the error message names the invalid sandbox field

#### Scenario: Invalid network value fails loudly
- **WHEN** a settings file contains an invalid sandbox network setting
- **THEN** loading fails with an error naming the offending file path
- **THEN** the error message names the sandbox network field

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

### Requirement: Exact permission fingerprint key remains a separate secret
The standard bootstrap SHALL load or create stable per-user exact-call
fingerprint key material outside home and project `settings.json`, and a direct
SDK session with remembered-rule persistence enabled SHALL use the same secret
lifecycle. The implementation SHALL open the parent and key through no-follow
file descriptors and validate before reading that the parent belongs to the
current user, is not group/other writable, and has no extended ACL; the key SHALL
be a current-user-owned regular `0600` file with one link, the expected length,
and no extended ACL. A private lifecycle lock SHALL serialize concurrent create
or rotation. New key material SHALL be written and synced completely before an
atomic no-backup publication. The public SDK SHALL also expose an additive
redacting value type for embedders to inject equivalent key material. The key
MUST NOT appear in settings, remembered rules, logs, formatted config, JSON
descriptors, or session descriptions. Platforms that cannot validate equivalent
ownership, link, mode, and ACL facts MUST fail closed.

#### Scenario: Standard bootstrap creates a private key
- **WHEN** bootstrap starts without explicitly injected fingerprint key material
- **THEN** it loads or creates `~/.coragent/permission-fingerprint.key`
- **THEN** the path is a regular `0600` file containing stable random key material

#### Scenario: Direct persistent SDK session gets reloadable exact rules
- **WHEN** a direct SDK caller enables remembered-rule persistence without injecting a key
- **THEN** standard session construction loads or creates the same private per-user key
- **THEN** an exact rule persisted by one session can match after restart

#### Scenario: Key representations are redacted
- **WHEN** injected fingerprint key material is formatted, JSON encoded, or sent to structured logging
- **THEN** the representation is explicitly redacted
- **THEN** neither raw nor encoded key material is exposed alongside a remembered rule

#### Scenario: Unsafe existing key is rotated without being trusted
- **WHEN** the key path is a symlink, has broad mode, wrong ownership, unexpected links, wrong length, or an extended ACL
- **AND** its parent directory is safe for replacement
- **THEN** the implementation does not read or chmod the old key
- **THEN** it removes all `exact-v1` and `exact-v2` selectors from raw home and project settings before atomically replacing the key without a backup
- **THEN** the lifecycle returns rotated status so the same session construction filters its already-loaded exact selectors
- **THEN** a secret-safe warning recommends rotating credentials that may have appeared in remembered exact calls

#### Scenario: Missing key invalidates persisted exact selectors
- **WHEN** no fingerprint key exists but home or project settings contain exact selectors
- **THEN** the selectors are scrubbed before a fresh key is published
- **THEN** the lifecycle returns fresh status so the same session construction also filters its in-memory exact selectors

#### Scenario: Unsafe parent fails closed
- **WHEN** the fingerprint-key parent has wrong ownership, group/other write access, or an extended ACL
- **THEN** session construction fails with actionable remediation
- **THEN** no existing key bytes are read and no replacement is published

### Requirement: Legacy loading behavior remains compatible
Adding public loading and bootstrap SHALL preserve the existing `Load`,
`LoadFrom`, defaults, environment-reference, merge, validation, missing-file, and
remembered-permission persistence behavior used by prior phases. Existing public
`SessionConfig`, `NewSession`, and `NewSessionWithError` callers MUST remain
source- and behavior-compatible except for the mandatory removal of unsafe
unkeyed exact-call selectors.

#### Scenario: Legacy Load discovers files
- **WHEN** an existing internal caller invokes the legacy `Load` operation with the same home directory, project directory, environment, and files as before Phase 7
- **THEN** it receives the same merged settings or equivalent error as before Phase 7 after unsafe `exact-v1` selectors are scrubbed

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
