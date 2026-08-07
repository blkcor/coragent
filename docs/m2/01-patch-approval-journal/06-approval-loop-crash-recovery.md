# S1.6 Engine Approval Loop and Crash Recovery

**Status:** pending acceptance
**Prerequisite:** [S1.5 accepted](05-action-journal-transcript.md)

## Goal

在 Engine run 循环中实现审批阻塞等待 + Resume 时的 action 崩溃恢复，
使 patch 的完整 Prepare → 审批 → Execute → 记录链路可运行。

## Deliverables

- `internal/engine/session.go`：run 循环中判断 `prepared.NeedsApproval()` 时：
  写入 `action_prepared` transcript → 发出 `approval_required` event →
  阻塞等待 approve/deny SessionCommand
- `waitForApproval(ctx, requestID) → (SessionCommand, error)` 方法：
  支持 context 取消（cancel SessionCommand 穿透审批等待）
- approve 路径：写入 `action_approved` → 写入 `action_committing` + fsync →
  执行 Action Broker Execute → 写入 `action_committed`
- deny 路径：写入 `action_denied` → 返回 tool_result(denied)
- Resume 崩溃恢复：扫描 transcript 中未闭合的 action record，执行恢复矩阵：

| Journal 最后状态 | 磁盘内容 vs 期望 | 恢复结果 |
|---|---|---|
| `action_prepared` 无 approved | — | tool_result(cancelled) |
| `action_approved` 无 committing | — | tool_result(cancelled) |
| `action_committing` 无 committed | = expected_sha256 | recovered_success |
| `action_committing` 无 committed | = source_sha256 | interrupted_no_effect |
| `action_committing` 无 committed | 都不匹配 | stale_aborted |
| `action_committed` | — | tool_result(success) |

## Acceptance

- [ ] 正常路径：Prepare → approve → Execute → committed → tool_result(success)
- [ ] 拒绝路径：Prepare → deny → tool_result(denied)，文件未被修改
- [ ] Cancel 穿透审批等待：cancel 命令终止阻塞的 waitForApproval
- [ ] 审批超时不适用（M2 审批无超时，用户想等多久等多久）
- [ ] Resume 恢复（模拟崩溃注入）：
  - [ ] committed → 补 tool_result(success)
  - [ ] committing + 磁盘 = expected → recovered_success
  - [ ] committing + 磁盘 = source → interrupted_no_effect，自动重试成功
  - [ ] committing + 磁盘不匹配 → stale_aborted
- [ ] 恢复后永不自动重放已 committed 的写入
- [ ] 离线测试：全路径覆盖 + `committing` 边界崩溃注入矩阵
- [ ] `go test -race ./...` 通过

## Evidence

Retain test output and crash-injection matrices under `artifacts/m2/s1/1.6/`.

## Design notes

- `interrupted_no_effect` 时自动重试不需要重新审批——原审批仍然有效，
  因为源文件未被外部修改
- `stale_aborted` 需要模型重新发起 tool call + 重新 prepare + 重新审批
- 崩溃恢复在 Resume 流程的早期执行，在任何新的模型请求之前（与 M1 reconcile 相同位置）
