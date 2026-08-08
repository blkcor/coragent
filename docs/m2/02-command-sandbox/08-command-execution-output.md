# S2.8 Command Execution + Output Pipeline

**Status:** pending
**Prerequisite:** [S2.7 accepted](07-command-tool-prepare.md)

## Goal

实现 Command 工具的 Execute 阶段：identity 校验、PTY sandbox 执行、进程组管理、输出处理（bounding → credential detection → projection → persistence）。复用 ActionBroker 的 ActionAttempt 日志和 crash recovery 框架。

## Deliverables

- `internal/tools/command_execute.go`：Execute 阶段逻辑
  - identity 校验：`current.Digest() == approved.Digest()` → 不匹配则 fail closed
  - identity 匹配但超过 expiry → fail closed（approve 过期机制）
  - 通过后：记录 `ActionAttempt{Status: "started"}` → sandbox.Start → 记录结果
- `internal/tools/command_output.go`：输出处理 pipeline
  - `boundOutput(raw []byte, maxBytes int64) ([]byte, bool)` — 截断 + truncated flag
  - `scanCredentials(raw []byte) []CredentialMatch` — 凭证检测（复用 M1 credential detector）
  - `redactOutput(raw []byte, matches []CredentialMatch) []byte` — 替换为标记
  - `buildProjections(raw, redacted []byte, truncated bool) (transcript, model, display []byte)` — 三种投影
- `internal/tools/command_persist.go`：大输出持久化
  - 完整输出写入 blob store（content-addressed, SHA256）
  - transcript 记录：bounded preview（前 N KB + SHA256 reference）
  - model context：bounded text（可能截断）
  - Event：display projection + redaction notice（如有凭证检测）
- `internal/tools/command_execute_test.go`：Execute 阶段测试
  - identity 不匹配 → 拒绝执行
  - 简单命令成功执行 → stdout/stderr 捕获
  - timeout → SIGKILL 进程组
  - context 取消 → SIGKILL 进程组
  - 输出截断 → truncated flag
  - 凭证检测 → redacted output
  - 大输出 → blob persist + transcript reference

### Execute 流程

```
PreparedAction (identity verified by ActionBroker)
    │
    v
Identity check: current.Identity.Digest() == approved.Identity.Digest()
    │ fail → stale result (不执行，返回 ToolResult{Stale: true})
    │
    v pass
Persist ActionAttempt{Status: "started", AttemptID, ToolCallID, IdentityDigest}
    │
    v
Sandbox.Start(ctx, CommandSpec)
    │
    ├── PTY ReadLoop: 实时读取 output
    │     ├── Size bound: 超过 MaxOutput 停止读取
    │     └── Timeout: ctx deadline → SIGKILL process group
    │
    v
Process.Result() → ProcessResult{ExitCode, Output, Duration, ConfinementLevel}
    │
    v
Output Pipeline:
    │
    ├── Credential scan on raw output
    ├── Build redacted projection
    ├── Persist full output to blob store (if large: > 4KB)
    ├── Build transcript record (bounded preview + ref)
    ├── Build model context projection
    └── Build display projection
    │
    v
Atomically finish ActionAttempt + append ToolResult to transcript
```

### Identity 校验

```go
func (c *CommandTool) verifyIdentity(
    prepared *PreparedCommand,
    current *CommandSpec,
    sandbox Sandbox,
) error {
    currentIdentity := ComputeIdentity(current, sandbox.ConfinementLevel())
    if currentIdentity.Digest() != prepared.Identity.Digest() {
        return fmt.Errorf("execution identity mismatch: approved %s != current %s",
            prepared.Identity.Digest(), currentIdentity.Digest())
    }
    return nil
}
```

## Acceptance

- [ ] identity 不匹配 → 拒绝执行，返回 stale result（ToolResult{Stale: true}）
- [ ] identity 匹配 → 通过 sandbox 执行命令
- [ ] `echo hello` → stdout 捕获 "hello"，exit code 0
- [ ] `exit 1` → exit code 1 捕获，ToolResult 标记为 error
- [ ] timeout → SIGKILL 进程组（父进程和所有子进程终止）
- [ ] context 取消 → SIGKILL 进程组
- [ ] 输出超过 MaxOutput → 截断 + truncated flag = true
- [ ] 凭证检测：输出含 API key pattern → redacted 替代
- [ ] 大输出（> persit threshold）→ 完整内容写入 blob store，transcript 保留 preview
- [ ] transcript/model/display 三种投影均不含 runtime secrets
- [ ] ActionAttempt 日志：started → terminal（success/error/timeout/stale）原子性
- [ ] 离线测试：所有路径 + 边界情况

## Evidence

Retain test output under `artifacts/m2/s2/2.7/`.

## Design notes

- ActionAttempt 持久化时机：**执行前**写入 started，**执行后**原子写入 terminal result。这和 S1 的 patch ActionAttempt 模式一致。
- identity 校验在 ActionAttempt 写入**之前**——避免持久化一个已知 stale 的 attempt。如果 identity 不匹配，直接返回 stale result，不写 ActionAttempt。
- 凭证检测复用 M1 `internal/credential/` 的 detector，加上 command output 特有的 pattern（如 `API_KEY=`、`token:` 等环境变量泄漏）。
- Output persistence threshold：S2 先用 4KB 作为阈值。小于阈值的输出直接存入 transcript record。大于阈值的写入 blob store，transcript 保留前 2KB preview + SHA256 ref。准确的阈值在 benchmark 反馈后调整。
- PTY ReadLoop 的 buffer 使用 ring buffer 或 `bytes.Buffer` + size check。超过 MaxOutput 后停止读取但不 kill 进程——进程可能仍在产生输出但已经不重要。如果进程在 timeout 前 exit，Result 返回完整的 buffered 输出（截断前）。
- `Process.Result()` 在进程 exit 后返回，等待时间 ≤ timeout + grace period (2s)。
