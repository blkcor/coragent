# S1.8 S1 Integration Acceptance

**Status:** pending acceptance
**Prerequisite:** [S1.7 accepted](07-cli-approval.md)

## Goal

端到端验证 S1 的完整链路：模型调查 + patch 提案 + 用户审批 + 执行写入 + 模型报告。
确认 M1 的只读功能未被破坏，M2 S1 的修改链路可正常工作。

## Deliverables

- 一条完整的端到端手工测试用例：在 Mercury fixture 中调查问题 → 模型调用 patch →
  用户审查 diff → 批准 → 文件被修改 → 模型报告结果
- 回归确认：M1 所有测试通过（`go test ./...` + `go test -race ./...`）
- 回归确认：M1 的 benchmark I01-I04 仍然可通过（分数不退化）
- 离线集成测试：scripted fake Provider 模拟完整的 approval 交互

## Acceptance

- [ ] 端到端手工测试通过：从用户 prompt 到文件修改到模型回复的完整流程
- [ ] 拒绝场景手工测试：deny → 文件未修改 → 模型可调整后重新 patch
- [ ] 崩溃恢复手工测试：在 patch 执行的不同阶段 kill 进程，resume 后正确恢复
- [x] M1 回归：`gofmt -w . && go test ./... && go test -race ./...` 全部通过 (356 passed, 2026-08-08)
- [x] M1 回归：`go build ./cmd/coragent && golangci-lint run ./...` 通过 (build ok; 12 pre-existing errcheck, no new issues)
- [ ] 如果 M1 benchmark 可运行，I01-I04 分数不退化 (不可运行: 无 provider endpoint)
- [x] 无新增 lint 警告或错误 (12 pre-existing errcheck in approval_test.go/session.go/approval_recovery_edge_test.go)

## Evidence

Retain CLI session transcripts and test output under `artifacts/m2/s1/1.8/`.

## Slice exit criterion

S1 可以合并当且仅当：
- 所有 8 个 checkpoint 的 checkbox 全部通过
- M1 回归测试全部通过
- 端到端手工测试通过
- 崩溃恢复矩阵全部通过

S1 合并后，用户可以：调查问题 → 审查 patch → 批准/拒绝 → 文件被修改 → 继续对话。
S2（command + sandbox）可以在 S1 合并后的分支上开始。
