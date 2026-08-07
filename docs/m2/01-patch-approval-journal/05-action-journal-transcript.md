# S1.5 Action Journal: Transcript Record Kinds

**Status:** accepted
**Prerequisite:** [S1.4 accepted](04-approval-protocol.md)

## Goal

扩展 Transcript，新增 6 种 action 生命周期 record kind，
使 crash recovery 能从单一 append-only 文件恢复所有 action 状态。

## Deliverables

- `internal/transcript/transcript.go`：新增 6 种 Record Kind：

| Kind | 关键字段 |
|---|---|
| `action_prepared` | request_id, tool_call_id, path, source_sha256, expected_sha256, diff_digest |
| `action_approved` | request_id, command_id |
| `action_denied` | request_id, command_id |
| `action_committing` | request_id |
| `action_committed` | request_id, actual_sha256 |
| `action_aborted` | request_id, reason |

- 每种 record 的 `Validate()` 方法
- diff 完整内容**不**存入 transcript——只存 `diff_digest` (sha256)
- `ValidateRecords` 扩展：action record 的配对规则（prepared → approved/denied/aborted，
  approved → committing → committed/aborted）

## Acceptance

- [x] 6 种 record 正确序列化/反序列化（严格 JSON，未知字段拒绝）
- [x] 每种 record Validate() 拒绝缺少必填字段的记录
- [x] ValidateRecords 接受合法的完整 action 生命周期链
- [x] ValidateRecords 拒绝：无 prepared 的 approved、无 approved 的 committing、
      重复的 committed
- [x] diff_digest 与 Prepare 阶段生成的 diff 内容一致（sha256 校验）
- [x] 离线测试：合法序列覆盖、非法序列拒绝、字段缺失拒绝

## Evidence

Retain test output and transcript fixtures under `artifacts/m2/s1/1.5/`.

## Design notes

- `action_committing` 是崩溃恢复的分界线——在执行文件写入**之前**写入并 fsync
- `action_aborted` 的 reason 取值为：`stale`、`cancelled`、`policy_block`、`denied`
- Transcript 已有 `AppendTranscript` + `appendJSONLines` + `fsync` 基础设施，新 record
  kind 直接复用
