# S1.3 Action Broker: EffectWrite and Two-Phase Execution

**Status:** pending acceptance
**Prerequisite:** [S1.2 accepted](02-patch-tool-prepare.md)

## Goal

移除 Action Broker 的 EffectRead 硬编码策略，新增 EffectWrite 支持，
实现 Prepare/Execute 两阶段执行模型。

## Deliverables

- `internal/action/broker.go`：移除 `EffectRead` 强制检查
- 新增 `EffectWrite` 常量（`internal/action/effects.go` 或 broker.go）
- Prepare 阶段：对 EffectWrite 工具调用，生成 PreparedAction（而非直接执行）
- Execute 阶段：接收 Prepare 阶段产出的 PreparedAction + 审批结果，
  校验 request_id，执行 stale detection，调用 FileService.Write
- `BlockedResult` / `SkippedResult` 继续用于 denial、policy block、stale 场景
- M1 的只读工具行为不变（EffectRead 工具跳过 Prepare/Execute 分阶段，直接执行）

## Acceptance

- [ ] EffectRead 工具（read/list/search）行为不变，M1 所有测试通过
- [ ] EffectWrite 工具（patch）Prepare 返回 PreparedAction，不执行写入
- [ ] EffectWrite 工具 Execute 执行实际写入
- [ ] 未知 Effect 的工具调用返回 blocked（policy）
- [ ] Stale detection：Execute 时 source_sha256 不匹配 → 返回 stale，不写入
- [ ] Execute 时 expected_sha256 与实际写入后文件哈希不匹配 → 返回错误
- [ ] 离线测试：Prepare/Execute 配对、stale 拒绝、policy block、未知 tool
- [ ] `go test -race ./...` 通过

## Evidence

Retain test output under `artifacts/m2/s1/1.3/`.

## Design notes

- S1.3 的 Execute 阶段还不写 transcript record（S1.5 统一处理）
- S1.3 的 Prepare 阶段不触发审批流程（S1.4/S1.6 处理）
- 现阶段 Execute 可以在测试中直接调用（无审批门），S1.6 加上审批门
