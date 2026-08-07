# S1.4 Approval Protocol: SessionCommand and Event

**Status:** pending acceptance
**Prerequisite:** [S1.3 accepted](03-broker-effect-write.md)

## Goal

定义审批通信协议：一个 Event（`approval_required`）和两个 SessionCommand
（`approve` / `deny`），复用 M1 的序列化和 ID 幂等机制。

## Deliverables

- `internal/sessioncommand/command.go`：新增 `KindApprove`、`KindDeny`
- 两个命令均包含 `request_id`（关联到 PreparedPatch.RequestID）
- `internal/event/event.go`：新增 `KindApprovalRequired`
- Event payload：request_id、tool_call_id、path、target、diff、is_sensitive、created_at
- 命令和事件的严格 JSON 解码 + 未知字段拒绝（复用 M1 `decodeStrict`）
- 重复 command ID 拒绝（复用 M1 `SeenCommands`）

## Acceptance

- [ ] `approve` 和 `deny` SessionCommand 正确序列化/反序列化
- [ ] `approval_required` Event 正确序列化/反序列化
- [ ] diff 字段可包含多行文本、Unicode、特殊字符（不损坏）
- [ ] 未知字段 → 解码失败
- [ ] 重复 command ID → 拒绝
- [ ] Event cursor 会话级单调递增（沿袭 M1 规则）
- [ ] 离线测试：序列化往返、ID 幂等、未知字段拒绝

## Evidence

Retain test output and serialized protocol fixtures under `artifacts/m2/s1/1.4/`.

## Design notes

- `approval_required` Event 的 diff 字段仅供 CLI 展示，**不**存入 transcript
- 没有 `revise` 命令（用户 deny 后在对话中指导模型重新生成 patch）
- 没有 `acknowledge` 命令（崩溃恢复用内容校验，S1.6）
- S1.4 只定义协议，Engine 审批循环在 S1.6 实现
