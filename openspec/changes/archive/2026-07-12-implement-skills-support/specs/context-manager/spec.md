# Context Manager Delta Specification

## ADDED Requirements

### Requirement: Skill content is tracked as a distinct context segment

The context manager SHALL track skill body injections as a distinct `skill`
segment type in the context-usage snapshot. When computing estimated usage, skill
content SHALL be included in the token count, and the snapshot SHALL report the
aggregate token contribution of all active skill segments for the current round.

#### Scenario: Skill segment appears in usage breakdown

- **WHEN** a skill body is injected into context for a model round
- **THEN** the context-usage snapshot includes a `skill` segment with the estimated token count

#### Scenario: Multiple skills produce multiple segment entries

- **WHEN** two skills are invoked in the same turn
- **THEN** the context-usage snapshot reports both skills as separate segments or an aggregated skill segment

#### Scenario: No skills means no skill segment

- **WHEN** no skills are invoked in a turn
- **THEN** the context-usage snapshot either omits the skill segment or reports it as zero

### Requirement: Skill segments do not persist across turns

Skill content tracked as context segments SHALL be scoped to the round in which
the skill was invoked. Subsequent rounds SHALL NOT include skill segments from
prior turns in their usage snapshots.

#### Scenario: Skill segment absent in next round

- **WHEN** a skill is invoked in round N but not in round N+1
- **THEN** round N's usage snapshot includes the skill segment
- **THEN** round N+1's usage snapshot does not include it
