# S2.9 Run Budget: Active Process Time

**Status:** pending
**Prerequisite:** [S2.7 accepted](07-command-execution-output.md)

## Goal

扩展 Run Budget 系统以追踪和限制活跃进程时间（active process time）。每次 command 执行的 wall-clock 时间计入预算，达到限额后暂停新 command 执行。复用 M1 的 Budget 持久化和 counter 防重置机制。

## Deliverables

- `internal/engine/budget.go`：扩展 `RunBudget`
  - 新增 `ActiveProcessTimeUsed time.Duration`
  - 新增 `ActiveProcessTimeLimit time.Duration`（默认 10 minutes）
  - `ReserveProcessTime(estimated time.Duration) (Reservation, error)` — 预留时间
  - `CommitProcessTime(used time.Duration)` — 执行后确认实际使用
  - `ReleaseProcessTime(reserved time.Duration)` — 预留未用完的释放
- Budget 计数器持久化：
  - `ActiveProcessTimeUsed` 在每次 command 完成后原子写入 store
  - process restart 不能重置 counter
- Budget 耗尽行为：
  - `ReserveProcessTime` 返回 `ErrBudgetExhausted`
  - Engine 将 budget exhausted 作为 tool result error 返回给模型
  - 模型可请求用户扩展 budget（通过 steering/approval 机制）
- `internal/engine/budget_test.go`：Process time budget 测试
  - 正常消耗 + 持久化
  - 耗尽 → 拒绝新 reservation
  - restart 后 counter 保持

### Process time 追踪

```
Command Execute 开始
    │
    v
ReserveProcessTime(CommandSpec.Timeout)
    │ budget exhausted → ToolResult{Error: "process time budget exhausted"}
    │
    v reservation OK
Sandbox.Start() → process running
    │
    v
Process.Result() → actualDuration (wall clock)
    │
    v
CommitProcessTime(actualDuration) → persisted
    │
    v
ToolResult (success/error)
```

## Acceptance

- [ ] `ActiveProcessTimeUsed` 初始为 0，limit 默认 10 minutes
- [ ] `ReserveProcessTime` 在限额内成功
- [ ] `ReserveProcessTime` 超过限额返回 `ErrBudgetExhausted`
- [ ] `CommitProcessTime` 正确累加实际用时
- [ ] `ReleaseProcessTime` 释放预留但未使用的时间（timeout 内提前完成）
- [ ] Budget 持久化：process restart 后 counter 不重置
- [ ] Budget 耗尽后 command 返回错误给模型（不是崩溃）
- [ ] 离线测试覆盖：正常消耗、耗尽、restart 保持、预留-释放

## Evidence

Retain test output under `artifacts/m2/s2/2.9/`.

## Design notes

- 为什么用 wall-clock time 而非 CPU time？CPU time 在 sandbox 内难以准确获取（尤其 PTY 场景），wall-clock 更直接反映用户等待体验。10 分钟默认值基于开发者日常命令的合理范围。
- `ReserveProcessTime` 用 `CommandSpec.Timeout` 作为估计值——这是最坏情况。实际执行可能提前完成，`ReleaseProcessTime` 退回差额。这避免了过于保守的预算计算。
- Active process time 只计入 command 工具的执行时间，不计入 patch 工具或 provider 调用时间（那些已有独立的预算计数器）。
- 预算耗尽后，模型收到 tool result error。模型可以告诉用户 "process time budget exhausted, please extend"。用户通过 SessionCommand（future：steering）扩展 budget。S2 阶段先实现拒绝+错误返回，扩展机制留到 M4 steering。
- 如果 command 在 timeout 前被用户 cancel，实际用时计入预算（已消耗的资源不能退回）。这和 retry delay 的预算逻辑一致：消耗不可逆。
