## ADDED Requirements

### Requirement: Prompt-submit hook gate
The loop SHALL invoke prompt-submit hooks before sending the user's prompt to the
provider. A blocking prompt-submit hook SHALL prevent provider work for that turn.

#### Scenario: Prompt-submit block stops provider call
- **WHEN** a run starts from a user prompt and a prompt-submit hook blocks that prompt
- **THEN** the provider is not called
- **THEN** the run stream surfaces the hook block reason

#### Scenario: Prompt-submit injection is visible to provider
- **WHEN** a prompt-submit hook injects context for a prompt
- **THEN** the injected context is included in the conversation assembled for the provider call

### Requirement: Session-start hook gate
The session lifecycle SHALL invoke session-start hooks before a session accepts
work. A blocking session-start hook SHALL prevent the session from running turns.

#### Scenario: Session-start block prevents runs
- **WHEN** a session-start hook blocks session startup
- **THEN** the session refuses to run user prompts
- **THEN** the caller receives the hook block reason

#### Scenario: Session-start injection seeds context
- **WHEN** a session-start hook injects standing context
- **THEN** that context is present before the first provider call in the session

### Requirement: Session-stop hook cleanup
The session lifecycle SHALL invoke session-stop hooks when the session stops or
closes. A blocking or failing session-stop hook SHALL be surfaced but SHALL NOT
prevent the session from stopping.

#### Scenario: Session-stop hook runs during cleanup
- **WHEN** a session is stopped or closed
- **THEN** matching session-stop hooks run during cleanup

#### Scenario: Session-stop block is recorded
- **WHEN** a session-stop hook blocks or fails
- **THEN** the block or failure is surfaced on the event stream or returned cleanup result
- **THEN** the session still stops

### Requirement: Run-finished hook notification point
The loop SHALL invoke run-finished hooks after computing a run's terminal
outcome and before emitting the terminal run-finished event. A blocking or
failing run-finished hook SHALL be surfaced but SHALL NOT change the run's
terminal outcome.

#### Scenario: Run-finished hook runs before terminal event
- **WHEN** a run reaches completed, failed, cancelled, or step-limit outcome
- **THEN** matching run-finished hooks run with that outcome
- **THEN** the terminal run-finished event is emitted after those hooks finish

#### Scenario: Run-finished hook failure preserves outcome
- **WHEN** a run-finished hook blocks or fails after the run outcome is known
- **THEN** the hook failure is surfaced on the run stream
- **THEN** the terminal run-finished event still carries the original outcome

### Requirement: Hook outcome events on run stream
The loop SHALL carry hook outcome events on the same run stream used for
assistant text, tool lifecycle, permission requests, status, and run completion.

#### Scenario: Frontend observes hook outcomes from stream
- **WHEN** a hook blocks, replaces content, or injects context during a run
- **THEN** the outcome appears on the run stream
- **THEN** a frontend can render it without reading internal hook state
