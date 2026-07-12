# slash-commands Specification

## Purpose
TBD - created by archiving change slash-commands. Update Purpose after archive.
## Requirements
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

### Requirement: `/exit` command

The system SHALL provide a `/exit` command (alias `/quit`) that cleanly shuts down the TUI, canceling any active run and denying any open permission prompt before closing the session.

#### Scenario: Idle exit
- **WHEN** the user types `/exit` while no run is active
- **THEN** the session closes cleanly and the TUI process exits

#### Scenario: Exit during active run
- **WHEN** the user types `/exit` while a run is in progress
- **THEN** the active run is cancelled, any open permission prompt is denied, and the session closes

### Requirement: `/skills` command

The system SHALL provide a `/skills` command (alias `/available-skills`) that lists loaded skills with their name, source (user/project), type, and description.

#### Scenario: Skills available
- **WHEN** the user types `/skills` and one or more skills are loaded in the session
- **THEN** the transcript displays each skill as a notice line showing name, source, type, and description

#### Scenario: No skills loaded
- **WHEN** the user types `/skills` and no skills are loaded
- **THEN** the transcript displays a notice: "No skills loaded."

### Requirement: `/context` command

The system SHALL provide a `/context` command (alias `/usage`) that displays the current context-window usage statistics from the most recent `EventContextUsage`.

#### Scenario: Usage available
- **WHEN** the user types `/context` and a context-usage event has been received
- **THEN** the transcript displays token usage, window size, percentage, and source (measured/estimated)

#### Scenario: No usage data yet
- **WHEN** the user types `/context` and no context-usage event has been received
- **THEN** the transcript displays a notice: "No context usage data available yet. Start a run."

### Requirement: `/mode` command

The system SHALL provide a `/mode` command that changes the permission mode when given a valid mode name.

#### Scenario: Valid mode switch
- **WHEN** the user types `/mode plan` and the session supports mode changes
- **THEN** the permission mode switches to plan mode and the footer reflects the change

#### Scenario: Invalid mode name
- **WHEN** the user types `/mode invalid`
- **THEN** the transcript displays a notice listing valid modes: default, auto, plan, bypass

#### Scenario: Mode change not supported
- **WHEN** the user types `/mode plan` and the session does not support mode changes (external ownership or custom dispatcher)
- **THEN** the transcript displays a notice: "Mode switching is not available in this session."

### Requirement: `/clear` command

The system SHALL provide a `/clear` command that clears the visible transcript locally.

#### Scenario: Clear transcript
- **WHEN** the user types `/clear`
- **THEN** the transcript is reset to empty and the startup hero is shown

### Requirement: `/help` command

The system SHALL provide a `/help` command (alias `/?`) that lists all available slash commands with their descriptions.

#### Scenario: Help listing
- **WHEN** the user types `/help`
- **THEN** the transcript displays a formatted list of all registered commands with their name(s) and description

### Requirement: Dynamic slash command registration

The slash registry SHALL support adding commands after initial construction, so skill entries can be registered when capability data arrives from the session. Registering a command with a name that already exists SHALL be a no-op for that name (built-in commands take precedence).

#### Scenario: Skills registered after construction
- **WHEN** the session startup completes and skill capabilities are available
- **THEN** each skill is registered as a slash command entry with kind "skill"
- **THEN** the `/help` command listing includes the newly registered skill entries

#### Scenario: Re-registration is idempotent
- **WHEN** `RegisterSkills` is called multiple times with the same skill set
- **THEN** no duplicate entries appear in the command list or suggestion dropdown

