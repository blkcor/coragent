# S1.1 FileService Abstraction and M1 Tool Refactor

**Status:** accepted
**Prerequisite:** M1 complete (all four slices merged)

## Goal

抽取统一的 `FileService` 接口，重构 M1 的三个只读工具（read/list/search）使用该接口，
为 S1.2 的 patch 工具提供共享的文件访问后端。

## Deliverables

- `FileService` 接口定义（`internal/workspace/fileservice.go`），包含 `Clean`、`Read`、
  `List`、`Search`、`Write`、`Identity` 六个方法
- 基于 M1 `workspace.Open`（`*FS`）的接口实现（`internal/workspace/fileservice_impl.go`）
- 重构 `internal/tools/tools.go`：read、list、search 工具改为通过 `FileService` 接口
  访问文件，不再直接调用 `os` 操作
- 所有路径经由 `os.Root` scoped filesystem 限制在 workspace root 内；拒绝 symlink、
  绝对路径、`..` 穿越
- M1 的 workspace confinement + TOCTOU 保护 + file identity 保持不变

## Acceptance

- [x] FileService 接口定义清晰，每个方法有明确的输入/输出/错误语义
- [x] read/list/search 通过 FileService 接口工作，行为与 M1 完全一致
- [x] 重构后所有 M1 测试通过（`go test ./...` + `go test -race ./...`）
- [x] 新增 FileService 实现的离线测试：路径穿越拒绝、symlink 拒绝、文件不存在、
      权限错误
- [x] Write 方法签名包含 `expectedSHA256` 参数（M1 阶段仅定义，S1.2 才调用）

## Evidence

Retain test output under `artifacts/m2/s1/1.1/`.

## Design notes

- `Write` 和 `Identity` 方法在 S1.1 仅定义接口 + 空实现或 panic-stub；
  S1.2 的 patch 工具是第一个调用方
- `List` 和 `Search` 保持现有纯 Go 实现，仅改为通过接口调用
- 这不是新功能，是纯重构——M1 所有行为保持不变
