# S2.8 CLI Command Approval Interaction

**Status:** pending
**Prerequisite:** [S2.7 accepted](07-command-execution-output.md)

## Goal

扩展 CLI 前端以展示 command 审批请求并处理用户的 approve/deny 响应。复用 S1 的 `approval_required` Event 和 `approve`/`deny` SessionCommand 机制，在 payload 层面区分 patch 审批和 command 审批。

## Deliverables

- `internal/event/event.go`：扩展 `KindApprovalRequired` Event payload
  - 新增 `ApprovalKind` 字段：`"patch"` 或 `"command"`
  - command 审批 payload 包含：
    - `command` — 完整命令行
    - `args` — 参数列表
    - `cwd` — 工作目录
    - `env_summary` — 环境变量摘要（keys only，不暴露 values）
    - `effect` — `"safe"` / `"workspace"` / `"dangerous"`
    - `policy_decision` — `"approve"` / `"deny"`
    - `execution_identity_digest` — identity SHA256（前 8 字符用于展示）
    - `timeout_seconds` — 超时时间
    - `max_output_kb` — 输出限制
    - `confinement_level` — 隔离级别
- CLI 前端（`cmd/coragent/`）：
  - 复用 S1 的审批显示逻辑（事件驱动）
  - command 审批展示格式：
    ```
    ╔══════════════════════════════════════════════╗
    ║  Command Approval Request                   ║
    ╠══════════════════════════════════════════════╣
    ║  Command:  go test ./...                    ║
    ║  Args:     [test, ./...]                    ║
    ║  CWD:      /Users/.../mercury               ║
    ║  Effect:   workspace-mutation               ║
    ║  Sandbox:  kernel (macOS Seatbelt)          ║
    ║  Timeout:  30s                              ║
    ║  Identity: a1b2c3d4                         ║
    ╠══════════════════════════════════════════════╣
    ║  [A]pprove  [D]eny                          ║
    ╚══════════════════════════════════════════════╝
    ```
  - 对于 `PolicyDeny` 的 command：显示 deny reason，不等待用户输入
  - 支持危险命令列表的展示
- 离线测试：CLI 审批渲染 + command approve/deny 交互

## Acceptance

- [ ] command `approval_required` Event 正确序列化/反序列化（含所有 command 特有字段）
- [ ] CLI 正确渲染 command 审批请求（命令、参数、CWD、effect 标签、sandbox 级别）
- [ ] CLI 正确处理 approve 输入 → `approve` SessionCommand 发出
- [ ] CLI 正确处理 deny 输入 → `deny` SessionCommand 发出
- [ ] `PolicyDeny` command → CLI 显示拒绝原因，不等待用户交互
- [ ] command 审批和 patch 审批可共存（同一 run 中先后出现时 CLI 正确切换）
- [ ] 离线测试：Event 序列化往返、CLI 渲染 snapshot（golden file）
- [ ] 复用 S1 的 command ID 幂等机制和 duplicate rejection

## Evidence

Retain test output and CLI interaction transcripts under `artifacts/m2/s2/2.8/`.

## Design notes

- CLI 只做展示和输入转发，不做 policy 决策。危险的展示（`rm -rf /`）是 CLI 的职责——用 `[DANGEROUS]` 标签醒目提示。
- `env_summary` 只展示 key names（如 `PATH, GOROOT, TMPDIR`），不展示 values——防止环境变量值包含敏感信息泄漏到 display。
- `execution_identity_digest` 在 CLI 展示时截断为前 8 字符（人类可读的短标识），但完整的 64 字符 SHA256 在 SessionCommand 中使用。
- command 审批 payload 和 patch 审批 payload 共用同一个 `KindApprovalRequired` Event kind，通过 `ApprovalKind` 字段区分。这保持了 Event 协议的向后兼容性和一致性。
- 当用户 approve 时，SessionCommand 携带 `revision_id`（execution identity digest），engine 侧用它在等待队列中匹配 PreparedAction。
