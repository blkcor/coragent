# S2.5 Policy Engine: allow/approve/deny + Session Memory

**Status:** pending
**Prerequisite:** [S2.4 accepted](04-effect-analyzer.md)

## Goal

实现 Policy Engine，根据 EffectAnalyzer 的分类结果和 session-scoped 记忆，对每个 command 输出 `allow`、`approve` 或 `deny` 决策。提供 session-scoped 审批记忆（workspace mutation 首次审批、后续同命令自动放行）。

## Deliverables

- `internal/policy/engine.go`：`PolicyEngine` 类型
  - `Decide(ctx context.Context, cmd CommandSpec, effect EffectClassification, session *SessionState) PolicyDecision`
  - `RecordApproval(cmd CommandSpec, decision PolicyDecision)`
- `internal/policy/decision.go`：决策类型
  - `PolicyDecision` 枚举：`PolicyAllow`、`PolicyApprove`、`PolicyDeny`
  - 每个 decision 附带原因字符串（用于 Event 和 transcript）
- `internal/policy/memory.go`：Session-scoped 审批记忆
  - `type SessionMemory struct { approvedPrefixes map[string]time.Time }`
  - key：`cmd + ":" + args[0:min(2, len(args))]`（如 `go:test`、`npm:install`）
  - 生命周期：非持久化，仅内存，进程退出后消失
  - `IsApproved(cmd, args []string) bool`
  - `MarkApproved(cmd, args []string)`
- `internal/policy/engine_test.go`：Policy Engine 测试

### 决策矩阵

| Effect | Session State | Decision | Reason |
|---|---|---|---|
| dangerous | any | **deny** | "dangerous command: `<cmd>` requires explicit user approval" |
| safe | any | **allow** | "safe read-only command" |
| workspace | first occurrence | **approve** | "workspace mutation requires approval" |
| workspace | subsequent (prefix match in memory) | **allow** | "previously approved workspace command: `<prefix>`" |
| unknown | any | **approve** | "unrecognized command, requires approval" |

### Session memory 行为

- 仅在 session 生命周期内有效（进程退出 → 记忆消失）
- key 为 command prefix（`<binary>:<arg0>:<arg1>`）
- 不持久化到 transcript 或 store（遵循 roadmap：M2 不 persist remembered decisions）
- `RecordApproval` 仅在 approval 被用户确认后调用
- approve 的 execution identity digest 随 memory 一起存储，用于后续 identity 校验

## Acceptance

- [ ] dangerous command → `PolicyDeny`（无论 session state 如何）
- [ ] safe command → `PolicyAllow`（无需审批，直接执行）
- [ ] workspace command 首次出现 → `PolicyApprove`
- [ ] workspace command 二次出现（相同 prefix）→ `PolicyAllow`
- [ ] session memory 不跨 session：新 session 中同样命令需要重新审批
- [ ] 不同 args 的 command 需要独立审批：`go test ./pkg/a` 和 `go test ./pkg/b` 共享 prefix `go:test`，一次审批后两者都放行（prefix-based matching）
- [ ] 未匹配分类的 command → `PolicyApprove`（保守 fallback）
- [ ] 离线测试覆盖：所有决策矩阵组合、session 记忆生命周期、prefix 匹配

## Evidence

Retain test output under `artifacts/m2/s2/2.5/`.

## Design notes

- Policy Engine 不做 I/O——它是纯函数（输入 CommandSpec + Effect + SessionState → Decision）。所有 I/O（Event 发射、transcript 写入）由 ActionBroker 负责。
- `PolicyDeny` vs `PolicyApprove` 的区别：dangerous 命令 S2 阶段返回 `PolicyDeny`（不允许执行），但未来可能加 `--allow-dangerous` flag 将其降级为 `PolicyApprove`。S2 不做这个 flag，dangerous = hard deny。
- prefix 匹配策略：`<binary>:<arg0>` 用于大部分命令（`go test`、`npm install`），某些命令可能需要 `<binary>:<arg0>:<arg1>`（`git checkout file` vs `git checkout branch`）。规则在 memory key builder 中定义。
- Session memory 是 memory 不是 policy——它记录已审批的 prefix，不替代 effect 分类。即使 `rm` 被 approve 过一次，dangerous 仍然每次要审批（memory 不缓存 dangerous）。
- Policy Engine 不负责 execution identity 计算——那是 command tool Prepare 阶段的职责（S2.6）。
