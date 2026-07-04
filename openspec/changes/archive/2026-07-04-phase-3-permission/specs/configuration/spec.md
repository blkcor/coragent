## ADDED Requirements

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
