# skill-execution Specification

## Purpose

Handles skill invocation — both user-triggered (`/skill-name` in chat input) and
model-triggered (tool call) — by injecting skill content into the agent's context
for the current turn.

## ADDED Requirements

### Requirement: User can invoke a skill with /name syntax

The system SHALL parse user input for `/skill-name` patterns before the agent loop
processes the turn. When a matching skill name is found, the skill body SHALL be
injected into context as a system-level message before the user's message reaches
the model. The `/name` token SHALL be stripped from the user-visible message.

#### Scenario: Single skill invocation

- **WHEN** the user sends `/linter review this code`
- **THEN** the `linter` skill body is injected as a system message
- **THEN** the model sees the skill content followed by the user message `review this code`

#### Scenario: Unknown skill name is passed through

- **WHEN** the user sends `/nonexistent do something`
- **THEN** the `/nonexistent` prefix is NOT stripped
- **THEN** the full text reaches the model unchanged (the model may interpret it as emphasis)

#### Scenario: Multiple /names invoke multiple skills

- **WHEN** the user sends `/linter /formatter fix this file`
- **THEN** both `linter` and `formatter` skill bodies are injected in registration order
- **THEN** the model sees `fix this file` as the user message

#### Scenario: /name at end of input is still recognized

- **WHEN** the user sends `fix this file /linter`
- **THEN** the `linter` skill is still invoked and the `/linter` token is stripped

### Requirement: Skills are registered as invocable tools

Each loaded skill SHALL be registered as a tool in the tool catalog. The tool name
SHALL be the skill name. The tool description SHALL come from the skill's
`description` field. When the model invokes the tool, the skill body SHALL be
injected into context as if the user had typed `/name`.

#### Scenario: Model invokes skill via tool call

- **WHEN** the model emits a tool call for skill `linter`
- **THEN** the `linter` skill body is injected as a system message
- **THEN** the tool result indicates success with the skill name

#### Scenario: Skill tool call flows through the execution chokepoint

- **WHEN** the model invokes a skill tool
- **THEN** the call flows through pre-hooks → permission → sandbox → execution → post-hooks
- **THEN** a blocking pre-hook can prevent the skill from loading

### Requirement: Skill content is scoped to the current turn

Skill content injected into context SHALL be scoped to the current turn and SHALL
NOT persist as a permanent system message for subsequent turns. Each invocation is
a one-shot injection.

#### Scenario: Skill content does not leak to next turn

- **WHEN** a skill is invoked in turn N
- **THEN** turn N+1 does not include the skill content unless the skill is invoked again

#### Scenario: Same skill invoked in consecutive turns

- **WHEN** a skill is invoked in turn N and turn N+1
- **THEN** each turn receives its own injection of the skill body
- **THEN** the two injections are independent

### Requirement: Skill content does not nest

Invoking a skill from within an already-active skill SHALL inject the second
skill's content but SHALL NOT recursively expand. A skill body that contains
`/other-skill` text SHALL be treated as literal text, not as a nested invocation.

#### Scenario: /name inside skill body is literal text

- **WHEN** skill `wrapper` has body text containing `/helper do something`
- **THEN** the text `/helper do something` is injected literally
- **THEN** no recursive skill resolution occurs
