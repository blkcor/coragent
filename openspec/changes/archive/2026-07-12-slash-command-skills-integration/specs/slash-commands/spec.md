## MODIFIED Requirements

### Requirement: Slash command dispatch

The TUI SHALL intercept composer input beginning with `/` before agent submission and dispatch it to a local command registry. Built-in commands execute without involving the model or starting a run. Skill commands (identified by a kind flag) SHALL NOT be dispatched locally; instead the raw input SHALL be submitted to the agent run path.

#### Scenario: Known command dispatched
- **WHEN** the user types `/help` and presses Enter
- **THEN** the system dispatches to the help handler without starting an agent run

#### Scenario: Skill command routes to agent
- **WHEN** the user types `/code-review` and the registry contains a skill entry for `code-review`
- **THEN** the system bypasses local dispatch and submits the raw input to `port.Run()` as a normal agent run
- **THEN** no "Unknown command" notice is produced

#### Scenario: Unknown command produces notice
- **WHEN** the user types `/unknown-command` and no built-in command or skill matches
- **THEN** the transcript displays a notice: "Unknown command: /unknown-command. Try /help."

#### Scenario: Non-slash input unaffected
- **WHEN** the user types ordinary text without a leading `/` and presses Enter
- **THEN** the system submits the input to the agent run as before

#### Scenario: Composer resets after slash command
- **WHEN** any slash command executes successfully
- **THEN** the composer is cleared and ready for the next input

## ADDED Requirements

### Requirement: Dynamic slash command registration

The slash registry SHALL support adding commands after initial construction, so skill entries can be registered when capability data arrives from the session. Registering a command with a name that already exists SHALL be a no-op for that name (built-in commands take precedence).

#### Scenario: Skills registered after construction
- **WHEN** the session startup completes and skill capabilities are available
- **THEN** each skill is registered as a slash command entry with kind "skill"
- **THEN** the `/help` command listing includes the newly registered skill entries

#### Scenario: Re-registration is idempotent
- **WHEN** `RegisterSkills` is called multiple times with the same skill set
- **THEN** no duplicate entries appear in the command list or suggestion dropdown
