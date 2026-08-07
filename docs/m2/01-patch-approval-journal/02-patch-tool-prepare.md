# S1.2 Patch Tool: Prepare Phase

**Status:** pending acceptance
**Prerequisite:** [S1.1 accepted](01-fileservice-refactor.md)

## Goal

实现 patch 工具的 Prepare 阶段：读文件、应用替换、生成 diff、计算哈希、扫描凭据，
产出 `PreparedPatch` 供审批使用。

## Deliverables

- `internal/tools/patch.go`：patch 工具的 Schema 定义 + Prepare 实现
- Schema：`path` (string, required)、`target` (string, required)、
  `replacement` (string, required)，无 `operation` 参数
- target 格式：`"L3-L5"`（替换3-5行）、`"L3"`（仅第3行）、`"L3-"`（3行到末尾）、
  `"L3-L3"`（在第3行前插入）
- Prepare 流程：通过 FileService 读文件 → 计算 source_sha256 → 按 target 定位行 →
  替换内容 → 计算 expected_sha256 → 生成 unified diff → 扫描凭据（复用 M1 Projector）
- `PreparedPatch` 结构体：RequestID、Path、Target、SourceSHA256、ExpectedSHA256、
  Diff、IsSensitive、CreatedAt
- Patch 工具注册到 Action Broker 的工具目录（先注册，S1.3 才放开 EffectWrite 执行）

## Acceptance

- [ ] path 存在 + target 有效 → 返回 PreparedPatch，包含正确的 diff 和哈希
- [ ] path 不存在 → 错误 "file not found"
- [ ] target 行号越界 → 错误 "line range out of bounds"
- [ ] target 格式非法 → 错误，明确指出格式要求
- [ ] 替换后文件末尾无多余空行（preserve trailing newline 语义）
- [ ] diff 包含凭据模式 → IsSensitive = true
- [ ] 大文件（接近 64KiB）→ 正常处理，输出被截断时有明确标记
- [ ] 离线测试覆盖所有 target 格式变体 + 边界条件（空文件、单行文件、末行替换）
- [ ] Prepare 阶段**不执行任何文件写入**

## Evidence

Retain test output, sample diffs, and credential-scan results under
`artifacts/m2/s1/1.2/`.

## Design notes

- Prepare 不写 transcript——transcript 记录由 Engine 在 Prepare 返回后统一写入（S1.5）
- `action_prepared` transcript record 的写入时机在 S1.5 定义，此处 Prepare 只返回数据
- target 定位使用 1-based 行号（与 M1 read 工具的 line-numbered output 一致）
