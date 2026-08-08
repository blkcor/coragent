# S2.7 Command Tool: Prepare + Execution Identity

**Status:** pending
**Prerequisite:** [S2.6 accepted](06-policy-engine.md)

## Goal

实现 Command 工具的 Prepare 阶段：解析命令、构建 `CommandSpec`、计算 execution identity、调用 EffectAnalyzer 和 PolicyEngine、生成用户预览。复用 Action Broker 的两阶段（Prepare → Execute）架构。

## Deliverables

- `internal/tools/command.go`：`CommandTool` 类型
  - `Prepare(ctx, toolCall) (PreparedAction, error)` — 无副作用
  - `Execute(ctx, preparedAction, sandbox) (ToolResult, error)` — 在 sandbox 中执行
- `internal/tools/command_prepare.go`：Prepare 阶段逻辑
  - 解析 tool call arguments → `CommandSpec`
  - 构建最小化环境变量（显式指定，不继承 host env）
  - 调用 `EffectAnalyzer.Classify`
  - 调用 `PolicyEngine.Decide`
  - 计算 execution identity digest
  - 生成用户预览（command + args + env summary + effect classification）
  - 返回 `PreparedCommand`
- `internal/tools/command_identity.go`：Execution identity
  - `type ExecutionIdentity struct { Command, Args, CWD, EnvKeys, EnvValues, SandboxLevel, PolicyVer }`
  - `Digest() string` — SHA256 序列化所有字段
  - Identity 序列化格式：`command\x00arg0\x00arg1\x00...\x00cwd\x00key=value\x00...\x00sandbox_level\x00policy_ver`
- `internal/action/prepared.go`：扩展 `PreparedAction`
  - 新增 `Effect EffectClassification` 字段
  - 新增 `Command *PreparedCommand` 字段（和 `Patch *PreparedPatch` 并列）
- `internal/tools/command_test.go`：Prepare 阶段测试
  - 正常参数解析
  - 环境变量构建
  - execution identity 确定性（相同输入 → 相同 digest）
  - execution identity 碰撞抗性（不同参数 → 不同 digest）

### PreparedCommand 结构

```go
type PreparedCommand struct {
    CommandSpec  CommandSpec
    Effect       EffectClassification
    Decision     PolicyDecision
    Identity     ExecutionIdentity
    Preview      string // 用户可读的命令预览
    RevisionID   string // 同 S1 PreparedPatch.RevisionID
}
```

### Prepare 流程

```
ToolCall.arguments (JSON)
    │
    v
Parse → CommandSpec {Command, Args, CWD, Env, Timeout, MaxOutput}
    │
    v
Validate: Command 非空、CWD 在 workspace 内、Env 不含 credentials
    │
    v
EffectAnalyzer.Classify(cmd, args) → EffectClassification
    │
    v
PolicyEngine.Decide(spec, effect, session) → PolicyDecision
    │
    ├── PolicyDeny → 返回 PreparedAction{Denied: true}，不生成 preview
    ├── PolicyAllow → 生成 preview，标记 auto-execute
    └── PolicyApprove → 生成 preview，标记 needs_approval
            │
            v
    Compute ExecutionIdentity.Digest()
            │
            v
    Build Preview — 用户可读的命令展示（命令 + 参数 + 环境变量摘要 + 分类标签）
            │
            v
    Return PreparedAction{Command: PreparedCommand, RevisionID: <uuid>}
```

## Acceptance

- [ ] Command 参数正确解析为 `CommandSpec`（command、args、cwd、env、timeout、maxOutputBytes）
- [ ] 环境变量构建：不含 `HOME`、`USER`、`PATH`（仅显式指定）、不含任何 credential 变量
- [ ] `EffectAnalyzer.Classify` 正确分类（safe/workspace/dangerous）
- [ ] `PolicyEngine.Decide` 输出正确决策
- [ ] `PolicyDeny` → PreparedAction 带有 `Denied: true`，不生成 identity
- [ ] `PolicyAllow` → PreparedAction 带有 `NeedsApproval: false`
- [ ] `PolicyApprove` → PreparedAction 带有 `NeedsApproval: true` + RevisionID
- [ ] Execution identity digest：相同输入 → 相同 digest，不同输入 → 不同 digest
- [ ] Preview 包含：command 字符串、args、分类标签（safe/workspace/dangerous）、CWD
- [ ] 离线测试：脚本化 fake PolicyEngine 和 EffectAnalyzer，覆盖所有 Prepare 路径

## Evidence

Retain test output under `artifacts/m2/s2/2.6/`.

## Design notes

- Prepare 阶段**不执行命令**。它只做解析、分类、决策、identity 计算和 preview 生成。
- `CommandSpec` 的环境变量是**显式构建的**，不继承 host 环境。架构文档明确要求 "minimal environment with no ambient credential variables"。`PATH`、`HOME` 等由 sandbox 层提供受控默认值。
- `RevisionID` 复用 S1 的模式：与 PreparedAction 绑定，审批过期后需重新 prepare。
- Execution identity 的 `SandboxLevel` 字段确保：同一个命令在 kernel sandbox 下审批的 identity 和 NOP sandbox 下的 identity 不同（不同隔离级别的审批不可替换）。
- Prepare 阶段如果遇到 `PolicyDeny`，不生成 preview（deny 是最终决策，不需要用户看 preview）。如果需要展示"为什么被 deny"，由 Event payload 传达。
- 参数 schema 在 tool declaration 中定义（OpenAI function calling schema）。validate 阶段检查必填字段 (`command`)、拒绝未知字段。
