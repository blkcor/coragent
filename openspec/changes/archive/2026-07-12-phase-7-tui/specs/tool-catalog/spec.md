## ADDED Requirements

### Requirement: Effective tool inventory is truthful and deterministic
The public session descriptor SHALL expose a deterministic tool inventory derived
from the session's effective advertised descriptors and executable ownership.
Each entry MUST use typed fields to state its name, source, advertised status,
executable status, and availability; it MUST NOT describe a descriptor-only or
handler-only mismatch as an available tool. For the default catalog, inventory
order SHALL follow effective advertisement order with any non-advertised
registered entries following in stable registration order.

#### Scenario: Advertised and executable tool is available
- **WHEN** the default catalog both advertises a descriptor and resolves its handler
- **THEN** the descriptor inventory contains one available tool entry with its built-in or caller source

#### Scenario: Advertised descriptor has no executable handler
- **WHEN** effective configuration advertises a tool name that the default catalog cannot resolve
- **THEN** the inventory marks the entry unavailable and states the mismatch
- **THEN** it does not claim that the capability is loaded and executable

#### Scenario: Registered handler is intentionally hidden
- **WHEN** the default catalog contains a handler omitted from the effective advertised list
- **THEN** the inventory marks that handler as not advertised and unavailable to the model
- **THEN** reporting it does not add it to provider descriptors

#### Scenario: Custom dispatcher ownership is not guessed
- **WHEN** a caller supplies a custom dispatcher without a capability reporter that can verify executable names
- **THEN** advertised entries identify caller ownership and unknown executability rather than claiming verified availability

#### Scenario: Inventory order is repeatable
- **WHEN** two equivalent sessions build descriptors from the same effective catalog and advertisement order
- **THEN** their tool inventory entries appear in the same order

### Requirement: Capability inventory never grants execution authority
Producing, registering, or reading capability inventory SHALL be descriptive
only. An inventory entry MUST NOT register a handler, advertise a descriptor to
the model, change permission or sandbox policy, add a name to a restricted child
catalog, or make an unavailable capability executable. Execution authority SHALL
continue to come only from the existing catalog, dispatcher, and safety chain.

#### Scenario: Reported tool cannot be invoked without a handler
- **WHEN** a capability reporter describes a tool that is absent from the executable catalog
- **THEN** the report does not make that tool resolvable or advertise it to the model

#### Scenario: Inventory does not widen a child view
- **WHEN** a parent inventory names capabilities outside a child's restricted advertised-and-executable set
- **THEN** those names remain absent from the child's executable catalog

#### Scenario: Reading inventory has no side effects
- **WHEN** a frontend obtains one or more session descriptor snapshots
- **THEN** tool registration, advertisement, ordering, and execution policy remain unchanged

### Requirement: Optional capability categories come only from reporters
The SDK SHALL allow an optional capability reporter to describe non-tool
categories such as skills or MCP servers using typed support, source, item, and
availability fields. With no reporter for a category, the session descriptor
MUST mark that category unsupported and MUST NOT display a fabricated loaded
count. A supported empty report SHALL remain distinguishable from unsupported.
Coragent v1 SHALL NOT implement a skills runtime or MCP client as part of this
reporting seam.

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
