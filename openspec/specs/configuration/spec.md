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
