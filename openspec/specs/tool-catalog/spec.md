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

