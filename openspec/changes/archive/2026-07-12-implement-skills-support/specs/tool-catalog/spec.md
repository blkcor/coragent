# tool-catalog Delta Specification

## ADDED Requirements

### Requirement: Post-initialization tool registration

The catalog SHALL accept tool registrations after the initial built-in set is
loaded, supporting dynamic registration from skill loading. Post-init registrations
SHALL follow the same rules as initial registrations: duplicate names are rejected
and the stable advertised order is maintained by appending new registrations after
existing ones.

#### Scenario: Skill registered after built-in tools

- **WHEN** built-in tools are already registered and a skill tool is registered afterward
- **THEN** the skill tool appears in the advertised list after all built-in tools

#### Scenario: Post-init duplicate is rejected

- **WHEN** a post-init registration attempts to use a name already in the catalog
- **THEN** the registration is rejected and the existing tool is unchanged

#### Scenario: Multiple post-init registrations are stable

- **WHEN** skills A, B, and C are registered in that order after built-in tools
- **THEN** the advertised list shows built-ins first, then A, B, C in registration order

### Requirement: Skill tools carry type metadata

Tools registered from skills SHALL carry the skill's `type` field in their
descriptor, exposing it as a `source` or `category` field that distinguishes them
from built-in tools. The type SHALL be passed through from the skill frontmatter
without the catalog interpreting it.

#### Scenario: Skill tool descriptor includes type

- **WHEN** a skill with type `project` is registered as a tool
- **THEN** the tool descriptor includes a field indicating it is a skill-sourced tool
- **THEN** the type value `project` is preserved in the descriptor

#### Scenario: Built-in tools are distinguishable from skill tools

- **WHEN** both built-in and skill-sourced tools are advertised
- **THEN** a consumer can distinguish them by inspecting the descriptor

## MODIFIED Requirements

### Requirement: Capability inventory never grants execution authority

Producing, registering, or reading capability inventory SHALL be descriptive
only. An inventory entry MUST NOT register a handler, advertise a descriptor to
the model, change permission or sandbox policy, add a name to a restricted child
catalog, or make an unavailable capability executable. Execution authority SHALL
continue to come only from the existing catalog, dispatcher, and safety chain.
Skill tool registration is a catalog operation, not an inventory operation; the
inventory reports skill tools but does not create them.

#### Scenario: Reported tool cannot be invoked without a handler

- **WHEN** a capability reporter describes a tool that is absent from the executable catalog
- **THEN** the report does not make that tool resolvable or advertise it to the model

#### Scenario: Inventory does not widen a child view

- **WHEN** a parent inventory names capabilities outside a child's restricted advertised-and-executable set
- **THEN** those names remain absent from the child's executable catalog

#### Scenario: Reading inventory has no side effects

- **WHEN** a frontend obtains one or more session descriptor snapshots
- **THEN** tool registration, advertisement, ordering, and execution policy remain unchanged

#### Scenario: Skill tool in inventory does not bypass registration

- **WHEN** inventory reports a skill tool but the skill was not registered through the catalog
- **THEN** the tool is not executable and the model cannot invoke it

### Requirement: Optional capability categories come only from reporters

The SDK SHALL allow an optional capability reporter to describe non-tool
categories such as skills or MCP servers using typed support, source, item, and
availability fields. With no reporter for a category, the session descriptor
MUST mark that category unsupported and MUST NOT display a fabricated loaded
count. A supported empty report SHALL remain distinguishable from unsupported.
When a skill runtime is active, it SHALL act as the skills capability reporter.

#### Scenario: No skills reporter means unsupported

- **WHEN** no capability reporter owns the skills category
- **THEN** the descriptor marks skills unsupported rather than reporting zero loaded skills

#### Scenario: Supported category is truthfully empty

- **WHEN** a reporter supports MCP inventory and reports no effective servers
- **THEN** the descriptor reports a supported empty MCP category with that reporter as source

#### Scenario: Reporter supplies available and unavailable items

- **WHEN** a custom reporter returns capability items with typed availability
- **THEN** the descriptor preserves each item's source and availability without converting unavailable entries into loaded counts

#### Scenario: Reporter does not create a runtime

- **WHEN** a reporter describes a skill or MCP server
- **THEN** Coragent does not execute, connect to, or advertise a model tool for it unless a separately authorized runtime already provides that capability

#### Scenario: Skill runtime serves as skills capability reporter

- **WHEN** the skill runtime is active with loaded skills
- **THEN** the session descriptor reports skills as a supported category
- **THEN** each skill appears as a capability item with its name, description, and type
