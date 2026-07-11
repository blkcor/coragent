## ADDED Requirements

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
