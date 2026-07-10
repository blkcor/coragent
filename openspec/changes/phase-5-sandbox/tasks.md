## 1. Policy and Configuration

- [x] 1.1 Add sandbox settings fields for extra read roots, extra write roots, and network access to the single JSON settings format
- [x] 1.2 Implement sandbox settings merge and validation with clear file/field errors
- [x] 1.3 Define internal sandbox policy inputs, policy output, and active confinement status types
- [x] 1.4 Implement deterministic policy derivation with path canonicalization, deduplication, stable ordering, and safe baseline roots
- [x] 1.5 Add unit tests for baseline policy derivation, additive grants, deterministic output, and invalid settings

## 2. Sandbox Backends

- [x] 2.1 Implement backend selection that chooses macOS OS enforcement when `sandbox-exec` is available and otherwise selects policy-based fallback
- [x] 2.2 Implement macOS Seatbelt profile generation from the derived policy
- [x] 2.3 Implement macOS command execution wrapper that runs command handlers under `sandbox-exec`
- [x] 2.4 Implement policy-based fallback behavior and downgrade reason reporting
- [x] 2.5 Add tests for profile generation, backend selection, fallback labeling, and fallback deny intent

## 3. Executor and Session Wiring

- [x] 3.1 Replace the default inert sandbox stage with the real sandbox stage during default session construction
- [x] 3.2 Preserve custom dispatcher behavior and existing hook/permission ordering
- [x] 3.3 Thread working directory, scratch temp root, sandbox settings, and permission context into policy derivation
- [x] 3.4 Ensure shell and command-declaring custom tools are sandboxed while read/write/edit/search/find tools continue to skip the sandbox stage
- [x] 3.5 Add executor tests for sandbox routing, permission denial before sandbox, hard-hook ordering, and sandbox block result handling

## 4. Runtime Reporting and Error Semantics

- [x] 4.1 Expose active confinement level and downgrade reason through SDK-visible session state or typed events
- [x] 4.2 Ensure sandbox blocks return readable recoverable tool errors tied to the originating tool call
- [x] 4.3 Preserve command timeout, cancellation, partial output, and process-group cleanup under sandboxed execution
- [x] 4.4 Add tests for blocked command recovery, cancellation, timeout, and no harness crash on backend errors

## 5. macOS Enforcement and Documentation

- [x] 5.1 Add macOS-gated tests proving in-project writes succeed and outside writes are blocked by OS-level sandboxing
- [x] 5.2 Add macOS-gated tests proving outbound network access is denied by default and allowed with an explicit grant
- [x] 5.3 Document sandbox settings fields, defaults, active confinement levels, and fallback caveats
- [x] 5.4 Run `gofmt`, `go test ./...`, and `openspec validate phase-5-sandbox`

## 6. Review Hardening

- [x] 6.1 Preserve command-handler validation and result semantics while requiring sandbox-mediated process execution
- [x] 6.2 Derive active tooling read roots and route temporary/build-cache writes through the scratch root
- [x] 6.3 Merge per-call sandbox grants with existing permission context additively
- [x] 6.4 Add direct timeout, cancellation, partial-output, backend-error, and ordinary Go-tooling regression coverage
