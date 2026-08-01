# S1.1 Serializable SessionCommand and Event Envelopes

**Status:** pending acceptance
**Prerequisite:** none

## Goal

Establish the smallest internal control and observation protocol used by the
line-oriented CLI and every later frontend.

## Deliverables

- Serializable `SessionCommand` envelope with command ID and the M1 `submit`
  and `cancel` kinds.
- Serializable `Event` envelope with session ID, run ID, session-wide cursor,
  timestamp, kind, and kind-specific payload.
- A minimal Session loop with at most one active run.
- A scripted fake Provider path that reaches a terminal run outcome.

## Acceptance

- [ ] Every M1 command and Event round-trips through the selected durable
      encoding without losing fields.
- [ ] A duplicate command ID is rejected without changing session state.
- [ ] One session permits at most one active run.
- [ ] A submitted fake-provider turn reaches one and only one terminal Event.
- [ ] Event payload tests prove channels, callbacks, credentials, internal
      pointers, and raw Go errors cannot enter the envelope.
- [ ] Tests run offline and deterministically.

## Evidence

Retain test output and the serialized protocol fixtures under
`artifacts/m1/s1/1.1/` with the tested Coragent commit.

## Failure and rollback boundary

This step performs no repository or user-state mutation. Protocol failures stop
the fake run with a typed cause. No public API is created.
