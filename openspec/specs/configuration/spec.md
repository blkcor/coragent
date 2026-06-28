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
