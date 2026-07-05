## ADDED Requirements

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
