## ADDED Requirements

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
