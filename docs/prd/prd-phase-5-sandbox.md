# Phase 5 — Sandbox (PRD)

> Implements node **5** of the dependency tree in `../roadmap.md`. Obeys
> `../architecture.md` as the canonical spec. Where this PRD and `../architecture.md`
> disagree, `../architecture.md` wins until amended.
>
> This is a **product requirements document**, written in user-story terms. It
> describes *what* the Sandbox must do and *why*, not how. Concrete data shapes,
> interface signatures, and package layout are designed in the planning step that
> immediately precedes implementation, per `../architecture.md` §10.

---

## 1. Introduction / Overview

Phase 5 gives coragent a **real confinement boundary** for the shell commands the
model runs. Until now, a command the agent decided to run executed with the full
privileges of the user who launched coragent: it could read any file, write
anywhere, and reach anywhere on the network. Phase 5 closes that gap.

When the agent runs a shell command, that command now executes **inside a
sandbox** governed by a policy describing what it may read, what it may write, and
whether it may reach the network. On macOS this is **OS-level confinement** —
enforced by the operating system kernel, so a command cannot escape it no matter
what it tries. Where that OS sandbox is unavailable, a **weaker, policy-based
confinement** applies behind the **same boundary**, so callers and the rest of the
harness see one consistent contract regardless of platform.

The policy is **derived automatically** from the project the agent is working in
(its working directory) plus configuration, with sensible safe defaults: the
command can write inside the project, read broadly enough to run normal tooling,
and **cannot reach the network** unless explicitly permitted.

This phase, together with Phase 4 hooks (`prd-phase-4-hooks.md`), delivers
Milestone **M3 — "It's safe"** (`../roadmap.md`): hard guardrails plus an OS
sandbox enforced on every shell call. The defining guarantee: **a command that
tries to write outside the project, or phone home, is stopped — and the model
cannot opt out.**

**Why this is the right cut.** The execution chokepoint (`../architecture.md` §6)
already isolates *where* shell commands run into a single sandbox stage. Phase 5
fills that stage with real enforcement without touching the loop, the executor,
the tools, permission, or hooks. Building it after Permission
(`prd-phase-3-permission.md`) and Hooks (`prd-phase-4-hooks.md`) means the policy
*inputs* — what the user is working on, what they have allowed — already exist to
derive the confinement from.

**Personas.**

- **Security-conscious TUI end-user.** Runs coragent as their daily coding agent
  on a real machine with real secrets (SSH keys, cloud credentials, other
  projects). Wants confidence that a command the model decided to run cannot touch
  things outside the project it is working on, and cannot phone home, even if the
  model is wrong, confused, or the command is malicious. Wants to be *told clearly*
  when the sandbox stops something, rather than discovering it through a cryptic
  failure.
- **SDK developer.** Builds their own frontend or automation on the coragent
  harness. Wants a dependable confinement boundary they can rely on for every
  shell command, with a single consistent behavior across platforms, and the
  freedom to swap in a stronger or different backend later without rewriting their
  integration. Wants to know, at runtime, *which* level of confinement is active.

---

## 2. Goals

- Confine **every shell command** the agent runs inside a sandbox stage on the
  single execution path (`../architecture.md` §6).
- On macOS, **block writes outside the project at the operating-system level** so
  the model cannot escape, no matter what the command tries.
- **Deny the network by default**; allow it only on an explicit grant.
- **Derive the policy automatically** from the working directory plus
  configuration, deterministically — the same inputs always produce the same
  policy.
- Allow configuration and permission grants to **only ever widen** the policy from
  a safe baseline, never weaken the deny-by-default floor.
- Provide a **weaker policy-based fallback** behind the same boundary when the OS
  sandbox is unavailable, honestly labeled as weaker.
- **Report the active confinement level** at runtime and **never crash** on an
  unsupported platform — the worst case is a labeled downgrade.
- Surface a sandbox-blocked, cancelled, or timed-out command as a **clear,
  recoverable tool error**, leaving no orphaned processes.
- Swap into the existing execution stage with **zero changes** to the loop,
  executor, tools, permission, or hooks.

---

## 3. User Stories

One session per story. Behavioral only — no types, no profile syntax.

### US-001: Confine writes to the project

**Description:** As a security-conscious end-user, I want a command the agent runs to write only inside the project I am working in (plus a scratch temp area), so that a stray or malicious command cannot modify, delete, or create files elsewhere on my machine.

**Acceptance Criteria:**
- [ ] A command writing inside the project directory succeeds and the file exists.
- [ ] A command writing to the scratch temp area succeeds.
- [ ] Confinement does not break legitimate in-project work.
- [ ] Build, typecheck, and unit tests pass

### US-002: Deny writes outside the allowed paths by default

**Description:** As a security-conscious end-user, I want writing anywhere outside the allowed paths to be denied by default, so that safety does not depend on me remembering to lock anything down.

**Acceptance Criteria:**
- [ ] A command writing to a path outside the project is blocked by the operating system on macOS.
- [ ] The target file is not created.
- [ ] The command returns a clear error result the agent can see.
- [ ] Build, typecheck, and unit tests pass

### US-003: Grant extra writable locations on demand

**Description:** As an end-user, I want to grant additional writable locations through configuration or an explicit permission decision when a task legitimately needs them, so that the default safety does not block real work.

**Acceptance Criteria:**
- [ ] A configured or permission-granted extra write root becomes writable inside the sandbox.
- [ ] The grant only adds to the baseline; the project-write + deny-by-default floor stays in force.
- [ ] No grant is required for ordinary in-project writes.
- [ ] Build, typecheck, and unit tests pass

### US-004: Read the project and system tooling

**Description:** As an end-user, I want commands to read the project and the system locations needed to run normal tooling (compilers, interpreters, standard libraries), so that ordinary build/test/lint commands just work inside the sandbox.

**Acceptance Criteria:**
- [ ] An ordinary build/test command can read the project and the system tooling locations it needs.
- [ ] The command runs normally under the sandbox without manual read configuration.
- [ ] Build, typecheck, and unit tests pass

### US-005: Broad reads, narrow writes

**Description:** As a security-conscious end-user, I want reading to be more permissive than writing, so that everyday tasks succeed while the genuinely dangerous operation — modifying my system — stays guarded.

**Acceptance Criteria:**
- [ ] Reads cover more locations than writes by default.
- [ ] A read permitted by the baseline does not imply a write to the same location.
- [ ] Everyday read-heavy tasks succeed while writes outside the project stay denied.
- [ ] Build, typecheck, and unit tests pass

### US-006: Add extra readable locations

**Description:** As an end-user, I want to add extra readable locations via configuration when a task needs them, so that I can opt into broader reads deliberately rather than by default.

**Acceptance Criteria:**
- [ ] A configured extra read root becomes readable inside the sandbox.
- [ ] The default read set is unchanged unless explicitly extended.
- [ ] Build, typecheck, and unit tests pass

### US-007: Deny the network by default

**Description:** As a security-conscious end-user, I want a command the agent runs to be unable to reach the network by default, so that it cannot exfiltrate my code, phone home, or pull in untrusted content without my knowledge.

**Acceptance Criteria:**
- [ ] A command attempting an outbound connection with no network grant fails on macOS.
- [ ] The failure is captured in the returned result.
- [ ] No network grant is in force unless explicitly enabled.
- [ ] Build, typecheck, and unit tests pass

### US-008: Explicitly allow the network

**Description:** As an end-user, I want to explicitly allow network access through configuration or a permission decision when a task needs it (for example, fetching dependencies), so that network-using work is possible but always a conscious choice.

**Acceptance Criteria:**
- [ ] With an explicit network grant, an outbound connection is permitted under the sandbox.
- [ ] Without a grant, the network stays denied.
- [ ] Build, typecheck, and unit tests pass

### US-009: Derive the policy from working directory and config

**Description:** As an end-user, I want the confinement policy to be derived automatically from the project I am working in plus my configuration, so that I get correct, safe confinement without hand-writing any policy.

**Acceptance Criteria:**
- [ ] The policy makes the project writable, the temp area writable, system locations readable, and the network denied — with no hand-written policy.
- [ ] The working directory and configuration are the inputs that shape the policy.
- [ ] Build, typecheck, and unit tests pass

### US-010: Deterministic policy derivation

**Description:** As an SDK developer, I want policy derivation to be deterministic — the same inputs always produce the same policy — so that confinement is predictable and testable.

**Acceptance Criteria:**
- [ ] Given a fixed working directory, configuration, and permission context, the derived policy is identical every time.
- [ ] The same inputs never produce a different policy.
- [ ] Build, typecheck, and unit tests pass

### US-011: Grants widen, never shrink below the floor

**Description:** As an SDK developer, I want configuration and permission grants to only ever widen the policy from a safe baseline, never silently weaken the deny-by-default posture below the project-write guarantee, so that I can reason about the floor of what is always enforced.

**Acceptance Criteria:**
- [ ] Extra read/write locations or a network grant only add to the baseline.
- [ ] The project-writable + deny-by-default floor is never removed by any grant.
- [ ] Build, typecheck, and unit tests pass

### US-012: One consistent boundary across platforms

**Description:** As an SDK developer, I want one consistent confinement boundary regardless of platform, so that my integration behaves the same whether or not a kernel sandbox is available, and I can target a single contract.

**Acceptance Criteria:**
- [ ] The same boundary contract is presented on every platform.
- [ ] The integration code path is identical whether the OS sandbox or the fallback is active.
- [ ] Build, typecheck, and unit tests pass

### US-013: Weaker fallback when the OS sandbox is unavailable

**Description:** As an end-user on a host without the OS sandbox, I want a weaker but still active policy-based confinement rather than no confinement at all, so that the same forbidden cases are still denied to the extent the platform allows.

**Acceptance Criteria:**
- [ ] On a host without the OS sandbox, the forbidden-write case is denied by policy and reported clearly.
- [ ] The fallback denies the same intent as the OS sandbox, with the honest caveat that it is weaker.
- [ ] Build, typecheck, and unit tests pass

### US-014: Report the active confinement level

**Description:** As both personas, I want coragent to tell me which level of confinement is active — real OS sandbox versus the weaker policy-based mode — so that I am never misled about how strong my protection actually is.

**Acceptance Criteria:**
- [ ] At initialization, coragent reports which confinement level is active.
- [ ] It never claims stronger protection than is actually in force.
- [ ] Build, typecheck, and unit tests pass

### US-015: Never crash on an unsupported platform

**Description:** As an end-user, I want coragent to never crash because a sandbox is unsupported on my platform; the worst case is a clearly-labeled downgrade to the weaker mode, so that the agent remains usable everywhere.

**Acceptance Criteria:**
- [ ] On a host where the OS sandbox is unavailable, coragent downgrades to the weaker mode with a clear label and still runs the command.
- [ ] It never panics or refuses to operate because confinement is unsupported.
- [ ] Build, typecheck, and unit tests pass

### US-016: Blocked commands come back as clear errors

**Description:** As an end-user, I want a command that the sandbox blocks to come back as a clear, readable error result the agent can see and react to — not a silent failure or a harness crash — so that I and the model both understand what happened and why.

**Acceptance Criteria:**
- [ ] A sandbox-blocked command returns a clear, readable error result, not a silent failure.
- [ ] The harness does not crash when the sandbox blocks a command.
- [ ] Build, typecheck, and unit tests pass

### US-017: Blocked commands are recoverable

**Description:** As an end-user, I want a blocked command to be reported as a normal tool error the agent can recover from (try a different approach, ask me), so that hitting the sandbox boundary is a recoverable event, not a dead end.

**Acceptance Criteria:**
- [ ] A blocked command is reported as a normal recoverable tool error, not a harness crash and not a silent success.
- [ ] The model can react to it (try a different approach or ask the user).
- [ ] Build, typecheck, and unit tests pass

### US-018: Clean cancellation and timeout

**Description:** As an SDK developer, I want a command that is cancelled or times out under the sandbox to be stopped cleanly, return whatever output it produced so far, and leave no orphaned processes behind, so that confinement does not introduce resource leaks.

**Acceptance Criteria:**
- [ ] A cancelled or timed-out command is stopped promptly.
- [ ] Any output produced so far is returned.
- [ ] No orphaned child processes remain.
- [ ] Build, typecheck, and unit tests pass

---

## 4. Functional Requirements

- **FR-1** — Every shell command the executor routes to the sandbox stage must run
  inside the sandbox; no shell command may bypass it.
- **FR-2** — On macOS, the sandbox must enforce the read/write/network policy at the
  operating-system level, such that a command cannot escape confinement.
- **FR-3** — The default policy must make the project directory and a scratch temp
  area writable, and deny all other writes.
- **FR-4** — The default policy must permit reads of the project and the system
  locations needed to run normal tooling, with reads strictly broader than writes.
- **FR-5** — The default policy must deny outbound network access.
- **FR-6** — The policy must be derived automatically from the working directory
  plus configuration plus permission context, deterministically.
- **FR-7** — Configuration and permission grants may add readable locations,
  writable locations, or network access; they must only widen the baseline and must
  never remove the project-write + deny-by-default floor.
- **FR-8** — When the OS sandbox is unavailable, the harness must apply a weaker
  policy-based confinement behind the same boundary, denying the same forbidden
  cases to the extent the platform allows.
- **FR-9** — The harness must report at runtime which confinement level is active
  and must never overstate the strength in force.
- **FR-10** — On an unsupported platform the harness must downgrade to the weaker
  mode with a clear label and continue to operate; it must never crash.
- **FR-11** — A command blocked by the sandbox must return a clear, recoverable tool
  error the model can react to, never a silent success or a harness crash.
- **FR-12** — A cancelled or timed-out command must be stopped promptly, return
  output produced so far, and leave no orphaned child processes.
- **FR-13** — The sandbox must integrate by filling the existing execution stage
  with zero changes to the loop, executor, tools, permission, or hooks.

---

## 5. Non-Goals (Out of Scope)

- **Non-macOS kernel sandboxes** (for example Linux Landlock or bubblewrap). Out of
  v1 per `../architecture.md` §1; the boundary is designed so they can be added
  later as additional backends without changing the contract.
- **Container / Docker-based confinement.** Strongest isolation and cross-platform
  parity, but heavyweight; admissible later behind the same boundary.
- **The executor chain, its stage ordering, and tool wiring** —
  `prd-phase-2-tools-executor.md`. Phase 5 only fills the sandbox stage.
- **The shell tool itself** and how it constructs a command —
  `prd-phase-2-tools-executor.md`. Phase 5 confines what the shell tool produces.
- **Permission allow/deny/ask logic** — `prd-phase-3-permission.md`. Phase 5 *reads*
  permission outcomes as policy inputs; it does not implement permission.
- **Hooks** — `prd-phase-4-hooks.md`. Hooks run *around* the sandbox stage; they may
  inform policy but are enforced elsewhere.
- **Confining read-only file tools** (such as grep/glob) under a read-only policy —
  noted as future hardening in `prd-phase-2-tools-executor.md`; the boundary allows
  opting them in later.
- **Live streaming of command output** — Phase 5 returns one final result; live
  streaming is a `prd-phase-7-tui.md` concern.

---

## 6. Design Considerations

- **One boundary, two strengths.** Callers see a single confinement contract; the
  difference between real OS enforcement and the weaker fallback is surfaced through
  the active-level report (US-014), never through a different integration path.
- **Safe by default, widen by choice.** The baseline is the floor: project
  writable, broad reads, network denied. Every grant is additive and visible, so a
  user reasons about *what was added*, not *what was removed*.
- **Honest reporting over silent strength.** When confinement downgrades, the user
  is told plainly. The system never lets a user believe protection is stronger than
  it is.
- **Recoverable boundary hits.** Reaching the sandbox edge is a normal, expected
  event the model can route around — never a dead end or a crash.

---

## 7. Technical Considerations

Conceptual only — no type definitions, no profile syntax.

- **macOS first backend.** The first real backend uses the macOS application
  sandbox facility (`sandbox-exec` / Seatbelt) to enforce read, write, and network
  policy at the kernel level, so a confined command cannot escape regardless of what
  it attempts.
- **Weaker policy-based fallback.** Where the OS sandbox is unavailable, a
  policy-based confinement applies the same deny intent behind the same boundary.
  Its network denial in particular is advisory in v1 — honestly surfaced via the
  active-level report rather than kernel-enforced.
- **Policy from inputs, not authored by hand.** Confinement is computed from the
  working directory, configuration, and permission context. The same inputs always
  yield the same policy, which makes derivation testable offline on every platform.
- **Slots into the existing stage.** The execution chokepoint
  (`../architecture.md` §6) already reserves a sandbox stage; Phase 5 replaces its
  no-op with real enforcement, leaving every surrounding component untouched.
- **Tested offline and hermetically** (`../architecture.md` §8). Policy derivation
  and the deny intent are exercised on every platform; the real OS-level enforcement
  is additionally exercised on macOS — all without a model or live network.

---

## 8. Success Metrics

Behavioral, aligned with `../architecture.md` §9 and Milestone **M3**
(`../roadmap.md`).

- **Confinement is real on macOS.** A command writing outside the project, or
  reaching the network without a grant, is blocked by the operating system and
  reported clearly.
- **The fallback denies the same intent.** On a host without the OS sandbox, the
  same forbidden cases are denied by policy and honestly labeled as weaker.
- **Legitimate work is unaffected.** In-project writes and normal build/test/lint
  commands succeed under the sandbox.
- **Safe by default, widen by choice.** Network denied and writes confined to the
  project out of the box; grants only widen, never breaching the floor.
- **Honest about strength.** The active confinement level is reported and never
  overstated.
- **Robust.** No crash on unsupported platforms; cancellation/timeout leaves no
  orphans; every blocked command is a recoverable tool error, never a panic.
- **Zero blast radius on the harness.** The sandbox swaps into the existing
  execution stage with no change to the loop, executor, tools, permission, or hooks.
- **Tested offline and hermetically.** Policy derivation and the deny intent are
  exercised on every platform; real OS enforcement is exercised on macOS — all
  without a model or live network.

A phase is done when its public surface is documented and stable, its behavior is
covered by offline tests against fakes, the acceptance criteria above pass, and it
integrates with prior phases without changing their public contracts
(`../architecture.md` §9).

---

## 9. Open Questions

- **Linux kernel sandboxes — out of v1.** Landlock (per-process filesystem rules)
  and bubblewrap (user-namespace jail) are the natural Linux backends; both fit
  behind the same confinement boundary as future backends, replacing the weaker
  policy-based mode on Linux with real kernel enforcement. The policy (read/write
  locations, network mode) maps cleanly onto them. Deferred.
- **Container / Docker backend — out of v1.** A container-per-command backend would
  give the strongest isolation and cross-platform parity, at the cost of a heavy
  dependency and added latency; admissible later behind the same boundary. Deferred.
- **Stronger fallback network enforcement.** The weaker policy-based mode's network
  denial is advisory in v1 (honestly surfaced via the active-level report). A future
  version could enforce it (for example via a network-isolating wrapper) rather than
  relying on policy alone.
- **Broadening default reads.** Today reads cover the project and program tooling
  locations; whether to default-allow reading the user's home directory (minus
  secret paths such as SSH and cloud-credential files) is a usability/safety
  tradeoff deferred to feedback. Extra read locations remain opt-in via
  configuration.
- **Per-command policy tightening from hooks.** A pre-tool hook
  (`prd-phase-4-hooks.md`) could narrow the policy for a specific command (force
  network-off, drop a write location). The derivation boundary can accept such
  constraints additively; the exact hook-to-policy plumbing is a follow-up.
- **Confining read-only file tools.** Routing file-touching tools (grep/glob)
  through a read-only policy (`prd-phase-2-tools-executor.md`) is admissible behind
  the same boundary; whether v1.x opts them in is open.
- **Live output streaming.** Phase 5 returns one final result; streaming live
  command output to the frontend is a `prd-phase-7-tui.md` concern, additive to this
  contract.
