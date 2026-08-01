# S1.3 Credential Isolation and Data Projection

**Status:** pending acceptance
**Prerequisite:** [S1.2 accepted](02-transcript-store.md)

## Goal

Make it safe to enable a real Provider endpoint by enforcing the `normal`,
`sensitive`, and `runtime_secret` projections first.

## Deliverables

- Dedicated Provider credential source.
- Versioned high-confidence credential detector.
- Protected-path structural projection.
- Redacted Transcript, Model Context, Event, log, and artifact projections.
- Incremental redaction for streamed assistant output.

## Acceptance

- [ ] The Provider credential reaches only the transport credential field and
      never enters a model message or tool argument.
- [ ] The versioned secret corpus is absent from Transcript, Model Context,
      Events, logs, persisted records, and test artifacts.
- [ ] Protected-path reads return redacted structural metadata and never persist
      raw content.
- [ ] Streamed assistant output is redacted before its first Event is emitted.
- [ ] Logs contain identifiers, classifications, sizes, and digests but no user,
      model, or tool content.
- [ ] A user-prompt credential match warns without echoing the value and sends
      no Provider request.

## Evidence

Retain the detector version, corpus digest, and projection test matrix under
`artifacts/m1/s1/1.3/`.

## Failure and rollback boundary

Classification failure stops the projection. Raw sensitive buffers are never
persisted. There is no override for sending detected credentials in M1.
