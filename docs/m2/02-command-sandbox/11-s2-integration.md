# S2.11 S2 Integration Acceptance

**Status:** pending
**Prerequisite:** [S2.1] through [S2.10] accepted

## Goal

端到端验证 S2 的完整链路：模型调查 → 命令提案 → 审批 → sandbox 执行 → 输出返回。确认 S1 的 patch + approval 功能未被破坏，M2 S2 的 command + sandbox 链路可正常工作。

## Deliverables

- 端到端手工测试用例集合（至少 5 条）：
  1. **safe 命令自动执行**：模型提议 `ls` 或 `git status` → 自动执行，无审批，输出返回给模型
  2. **workspace 命令审批**：模型提议 `go test ./...` → 用户审批 → sandbox 执行 → 测试输出返回
  3. **workspace 命令二次放行**：同 session 内再次 `go test ./...` → 自动执行（session memory）
  4. **dangerous 命令拒绝**：模型提议 `rm -rf /tmp/test` → Policy Engine deny → 模型收到拒绝原因
  5. **sandbox 路径限制**：命令尝试写入 workspace 外路径 → sandbox 拒绝 → 错误返回
- 离线集成测试：
  - scripted fake Provider 模拟完整 command 交互
  - fake PolicyEngine 模拟各种决策
  - fake Sandbox 模拟执行和输出
  - 覆盖：safe、workspace-first、workspace-second、dangerous-deny、identity-mismatch、timeout、output-too-large
- 回归确认：
  - S1 所有测试通过（`go test ./...` + `go test -race ./...`）
  - S1 benchmark I01-I04 不退化（若可运行）
  - S1 端到端 patch 流程不破坏
  - `golangci-lint run ./...` 无新增问题
- S2 slice exit criterion：见下方

## Acceptance

- [ ] 端到端 1：safe 命令自动执行通过
- [ ] 端到端 2：workspace 命令审批 → 执行通过
- [ ] 端到端 3：二次同命令自动放行通过
- [ ] 端到端 4：dangerous 命令被 deny 通过
- [ ] 端到端 5：sandbox 限制生效通过
- [ ] 离线集成测试全部通过（scripted fake Provider + fake PolicyEngine + fake Sandbox）
- [ ] S1 回归：`go test ./...` + `go test -race ./...` 全部通过
- [ ] S1 回归：`go build ./cmd/coragent` + `golangci-lint run ./...` 通过
- [ ] S1 端到端 patch 流程未被破坏（手动验证或自动化）
- [ ] crash recovery 矩阵：command 执行前/中/后 kill 进程，resume 后不 replay command
- [ ] 无新增 lint 警告或错误

## Slice exit criterion

S2 可以合并当且仅当：
- 所有 10 个 checkpoint 的 checkbox 全部通过
- S1 回归测试全部通过
- 端到端手工测试全部通过
- Crash recovery 矩阵全部通过（command 从不自动 replay）
- 至少一个平台有 kernel 级 sandbox（macOS Seatbelt）并通过测试

S2 合并后，用户可以：调查问题 → 模型提出 patch 或 command → 审查 → 批准/拒绝 → 在 sandbox 中执行 → 模型报告结果。

## Evidence

Retain CLI session transcripts, test output, and sandbox verification under `artifacts/m2/s2/2.10/`.

## Design notes

- S2 集成测试的 sandbox 用 NOP（平台无关），但手工测试应在 macOS（Seatbelt）和 Linux（Landlock+seccomp）上验证 kernel 级隔离。Windows 平台手工测试验证 ConPTY PTY 支持和进程组管理（job object）。
- `PolicyDeny` 场景中，模型应收到有意义的拒绝原因（不只是 "denied"），以便模型调整策略（如建议用更安全的命令替代）。
- crash recovery 对于 command 的关键行为：command 从不自动 replay。resume 后 `started` 的 ActionAttempt 被标记为 `interrupted`，模型看到 "previous command was interrupted" 结果后自行决定是否重试。
- S2 完成后，S3（benchmark suite）在此基础上运行全部 12 项 benchmark 任务。S2 的实现质量直接影响 M2 premise gate 的 26/36 分数。
