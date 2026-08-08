# S1.7 CLI Approval Interaction

**Status:** accepted
**Prerequisite:** [S1.6 accepted](06-approval-loop-crash-recovery.md)

## Goal

在 CLI 前端渲染 `approval_required` 事件并接受用户的 approve/deny 输入，
完成人机审批交互闭环。

## Deliverables

- `cmd/coragent/main.go`：订阅 `approval_required` 事件，渲染彩色 unified diff
- 审批提示格式：渲染 diff 后显示选项行 `[a] Approve  [d] Deny`，
  用户输入单个字母确认
- 用户输入 `a`（大小写不敏感）→ 发送 `approve` SessionCommand（带 request_id）
- 用户输入 `d`（大小写不敏感）→ 发送 `deny` SessionCommand（带 request_id）
- 其他任意输入 → 重新显示选项行，提示 `Press 'a' to approve or 'd' to deny`
- `is_sensitive = true` 时 diff 显示为 `[BLOCKED: credential detected in patch]`
- 审批响应通过现有 SessionCommand 通道发送（不新增 CLI 输入模式）

## Acceptance

- [x] `approval_required` 事件到达时，CLI 渲染 diff 预览 + 选项行并等待用户输入
- [x] 输入 `a` → Engine 收到 approve 命令，继续执行
- [x] 输入 `d` → Engine 收到 deny 命令，返回 tool_result(denied)
- [x] 输入无效字符 → 重新显示选项行，不发送命令
- [x] `is_sensitive = true` → diff 被替换为 blocked 消息，但仍显示选项行
- [x] CLI 在审批等待期间仍可接收 cancel（Ctrl+C）并终止 run
- [x] 手工测试：启动 coragent，触发 patch 调用，审查 diff，按 `a` 批准，验证文件已修改

## Evidence

Retain CLI session transcripts (input/output) under `artifacts/m2/s1/1.7/`:

- `test_output.txt`：`cmd/coragent` 包 `TestCLIApproval*` 五个验收场景的完整
  CLI 会话 transcript（approve / deny / 无效输入重提示 / 敏感 diff 阻断 /
  审批等待中 Ctrl+C 取消并 resume）
- `go-test.txt`、`go-test-race.txt`：全量离线测试与 race 测试结果

## Design notes

- CLI 不解析 diff 内容——只做语法高亮渲染。diff 的语义完全由 engine 负责
- 审批等待期间 CLI 处于特殊的 `[a/d] >` 提示符，区别于正常的 `>` 提示符
- 单字母输入（`a`/`d`）映射为对应的 SessionCommand（`approve`/`deny`），
  Engine 和协议层不感知 CLI 的简化交互方式
- 复用 M1 的 Event 订阅机制（`observeEvents`），不新增通信通道
- 空闲提示符与审批回答共享同一个 `bufio.Reader`（验收时发现每次审批读取
  新建 reader 会吞掉管道输入中已预读的后续行，已在 S1.7 验收中修复）
