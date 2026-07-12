# tool-catalog Specification

## Purpose
TBD - created by archiving change phase-2-tools-executor. Update Purpose after archive.
## Requirements
### Requirement: Registration advertises exactly the registered set

The catalog SHALL register tools by name and advertise to the agent exactly one
entry per registered tool and nothing else.

#### Scenario: Advertised list matches the registered set

- **WHEN** a set of tools is registered and the catalog is asked for the list advertised to the agent
- **THEN** the list contains one entry per registered tool and no others

### Requirement: Stable advertised order

The advertised tool order SHALL be identical across runs for the same set of
registered tools, so runs are reproducible.

#### Scenario: Order is stable across runs

- **WHEN** the same set of tools is registered and advertised on two separate runs
- **THEN** the advertised order is identical between the runs

### Requirement: Duplicate names rejected at registration

Registering a second tool under an already-used name SHALL be rejected at
registration time, and the first tool SHALL NOT be lost.

#### Scenario: Second registration under a used name is rejected

- **WHEN** a tool is registered under a name already in use
- **THEN** the registration is rejected at wire-up time and the first tool remains registered

### Requirement: Deterministic restricted catalog views
The tool catalog SHALL produce an internal restricted view from a requested name
set by retaining only matching registered handlers that are also present in the
parent's effective advertised descriptor list. The view SHALL preserve the exact
parent descriptor and parent advertisement order for each retained handler. Its
advertised descriptors and executable lookup set MUST describe the same names,
and creating the view MUST NOT mutate the parent catalog.

#### Scenario: Restricted view preserves parent order
- **WHEN** a parent effectively advertises tools A, B, and C and a child requests C and A
- **THEN** the child view advertises and resolves A then C in the parent's stable advertised order

#### Scenario: Restricted view preserves caller descriptor
- **WHEN** the parent advertises a caller-supplied descriptor for a registered handler
- **THEN** the child view retains that exact descriptor rather than regenerating one from the handler

#### Scenario: First duplicate descriptor wins for child derivation
- **WHEN** the parent advertised list contains the same executable tool name more than once
- **THEN** the child view retains the first descriptor for that name, contains one handler for it, and leaves the root advertised list unchanged

#### Scenario: Unknown requested name is absent
- **WHEN** a child requests a name that has no handler in the parent catalog
- **THEN** the restricted view neither advertises nor resolves that name

#### Scenario: Unadvertised handler is absent
- **WHEN** the parent catalog can execute a handler that the parent descriptor list does not advertise
- **THEN** the restricted view neither advertises nor resolves that handler

#### Scenario: Parent catalog remains unchanged
- **WHEN** one or more restricted child views are created
- **THEN** the parent catalog continues to advertise and resolve its original registered set

### Requirement: Safe read-only default view
When subagent selection supplies no names, the catalog SHALL derive a default
view containing only effective parent tools named `read_file`, `search_content`,
and `find_files`, in parent advertisement order. It MUST NOT infer read safety
for custom tools or include write, edit, or command handlers.

#### Scenario: Default view contains the three known read handlers
- **WHEN** the parent has all standard built-ins and the child requests no tools
- **THEN** the restricted view contains `read_file`, `search_content`, and `find_files` only

#### Scenario: Missing default handler is skipped
- **WHEN** the parent catalog does not contain one of the known read-only default names
- **THEN** the restricted view contains the remaining available default handlers without creating a substitute

#### Scenario: Custom read-classified handler is not implicit
- **WHEN** the parent has a custom handler classified as read-only and the child requests no tools
- **THEN** the custom handler is absent until it is explicitly requested by name

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

