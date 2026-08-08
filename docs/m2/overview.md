# Coragent V2 M2 Delivery Plan

## Status and authority

This directory is the implementation handoff for M2: **Safe Change Loop**.
Each numbered document is one mandatory acceptance checkpoint.
M2 is split into three independently mergeable vertical slices:
S1 (patch + approval + journal), S2 (command + sandbox), S3 (benchmark suite).

The source contracts remain authoritative:

- [`docs/product.md`](../product.md)
- [`docs/architecture.md`](../architecture.md)
- [`docs/roadmap.md`](../roadmap.md)
- [`docs/benchmarks.md`](../benchmarks.md)

Design decisions settled during M2 planning:

1. **审批同步阻塞** — engine 发出 `approval_required` 事件后阻塞等待，审批不跨 session 持久化
2. **只有 approve/deny** — 不提供 revise（参数修改由模型负责），不提供"记住"（M3+）
3. **Action Journal 扩展 Transcript** — 新增 6 种 record kind，同一 `transcript.jsonl` 文件
4. **崩溃恢复用内容校验** — 不提供 acknowledge SessionCommand，系统用 source/expected SHA256 自动判断
5. **Patch 纯 replace** — 去掉文件创建语义，path 不存在直接失败

## Product outcome

M2 让 Coragent 在只读调查之外能够执行受审查的文件修改。用户批准一个精确的 patch
预览后，系统安全地执行写入。崩溃恢复不需要用户参与。

S1 完成后，用户可以通过 CLI 完成：调查问题 → 模型提出修改 → 审查 diff → 批准/拒绝 →
执行写入 → 模型报告结果。

## Non-goals (M2 scope gate)

- TUI, Bubble Tea, full-screen layout
- Command execution, process supervision, OS sandbox (S2)
- File creation tool (`create`)
- "同意并记住" / persisted approval decisions (M3+)
- `revise` / `acknowledge` SessionCommands
- Network grants / external roots (S2)
- Context compaction, long-task recovery (M3)
- Public SDK or `pkg/coragent/` (M5)
- M2 benchmark suite E01-E04, F01-F02, R01-R02 (S3)

## Settled decisions

1. Approval is synchronous and expires with the prepared action.
2. Action lifecycle records live in Transcript, not a separate journal file.
3. Diff content is transient (Event only), not stored in Transcript.
4. Crash recovery for file mutations is fully automated via content-identity verification.
5. Patch tool only modifies existing files; creation is a separate future tool.
6. M1's `ApprovalRequired` / `approve` / `deny` protocol replaces the roadmap's
   planned `acknowledge` command.
7. All file tools share one `FileService` interface; patch goes through the same
   workspace confinement as read/list/search.

## How to execute this plan

- Complete documents in numeric order within each slice.
- A document is accepted only when every checkbox in its `Acceptance` section
  passes and its evidence is retained.
- Do not begin the next numbered document after a failed checkpoint.
- A slice may merge only after its exit criterion passes. Every merged slice
  must remain runnable and useful if later slices never land.
- Offline tests use a scripted fake Provider, fake clock, and `t.TempDir()`;
  they never contact a real model or user state.
- Step evidence lives under `artifacts/m2/s1/` (and `s2/`, `s3/`). Not committed.

## Ordered checkpoints

### Slice 1: Patch Tool + Approval + Action Journal

1. [FileService abstraction and M1 tool refactor](01-patch-approval-journal/01-fileservice-refactor.md)
2. [Patch tool: Prepare phase](01-patch-approval-journal/02-patch-tool-prepare.md)
3. [Action Broker: EffectWrite and two-phase execution](01-patch-approval-journal/03-broker-effect-write.md)
4. [Approval protocol: SessionCommand and Event](01-patch-approval-journal/04-approval-protocol.md)
5. [Action Journal: Transcript record kinds](01-patch-approval-journal/05-action-journal-transcript.md)
6. [Engine approval loop and crash recovery](01-patch-approval-journal/06-approval-loop-crash-recovery.md)
7. [CLI approval interaction](01-patch-approval-journal/07-cli-approval.md)
8. [S1 integration acceptance](01-patch-approval-journal/08-s1-integration.md)

### Slice 2: Command Tool + Sandbox

Design documents in `02-command-sandbox/`.

1. [Sandbox Runtime Interface + NOP](02-command-sandbox/01-sandbox-interface.md)
2. [macOS Sandbox: Seatbelt](02-command-sandbox/02-macos-sandbox.md)
3. [Linux Sandbox: Landlock + seccomp](02-command-sandbox/03-linux-sandbox.md)
4. [Windows Sandbox: NOP + ConPTY](02-command-sandbox/03-windows-sandbox.md)
5. [Effect Analyzer: Pattern-based Classification](02-command-sandbox/04-effect-analyzer.md)
6. [Policy Engine + Session Memory](02-command-sandbox/05-policy-engine.md)
7. [Command Tool: Prepare + Execution Identity](02-command-sandbox/06-command-tool-prepare.md)
8. [Command Execution + Output Pipeline](02-command-sandbox/07-command-execution-output.md)
9. [CLI Command Approval](02-command-sandbox/08-cli-command-approval.md)
10. [Run Budget: Active Process Time](02-command-sandbox/09-run-budget-process-time.md)
11. [S2 Integration Acceptance](02-command-sandbox/10-s2-integration.md)

Architecture:

```
Tool Request → Effect Analyzer → Policy Engine → allow/approve/deny
                                                    │
                                                    v
                                           Sandbox Runtime
                                           ┌──────────────┐
                                           │  PTY Manager  │
                                           │  master↔slave │
                                           └──────┬───────┘
                                                  │
                                           Platform Backend
                                           macos / linux / windows (nop)
```

Key design decisions:
- Sandbox as platform-independent interface with per-platform backends (macOS Seatbelt, Linux Landlock+seccomp, Windows NOP+ConPTY)
- Three-tier command classification: safe (auto-allow), workspace (approve once, then session-allow), dangerous (always deny)
- Pattern-based Effect Analyzer: deterministic rules, dangerous rules cannot be overridden by the model
- All commands execute via PTY master/slave (Unix: posix_openpt, Windows: ConPTY, pipe fallback on unsupported versions)
- Execution identity: SHA256(command + args + cwd + env + sandbox level) — analogous to patch content identity
- Session-scoped approval memory (in-memory only, not persisted across sessions)

### Slice 3: Benchmark Suite (planned, not designed)

Design deferred. Scope from roadmap:

- E01-E04 focused-edit tasks
- F01-F02 failing-test repair tasks
- R01-R02 tool-recovery tasks
- Three 12-slot rounds through line-oriented CLI
- 26/36 premise gate with category floors
- M2 permission script (workspace_write)
- Benchmark runner adapted from `m1bench`

## Comparability rule

M2's 36-slot mixed-task report and M1's 12-slot investigation report are not
directly comparable. Longitudinal comparison is permitted only for the I01-I04
panel when the Mercury base, task prompt, scorer, and unseeded workspace digest
are unchanged. M2 permission scripts are additive to the M1 read-only baseline.

## External requirements

Same as M1: one fixed OpenAI-compatible endpoint, one dedicated Provider credential,
an immutable model snapshot supporting streaming ToolCalls and tool-result continuation,
at least 32,000 input tokens and 8,000 output tokens, the Go toolchain, and `golangci-lint`.
