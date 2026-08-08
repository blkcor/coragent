# S2.2 macOS Sandbox: Seatbelt Backend

**Status:** accepted (2026-08-08)
**Prerequisite:** [S2.1 accepted](01-sandbox-interface.md)

## Goal

实现 macOS 平台 `Sandbox` 接口，使用系统内置 `sandbox-exec` 和 Seatbelt profile 提供 kernel 级 (`ConfinementKernel`) 进程隔离。这是 V2 GA macOS 认证的关键组件。

## Deliverables

- `internal/sandbox/macos/sandbox.go`：`Sandbox` 接口的 Seatbelt 实现
- `internal/sandbox/macos/profile.go`：Seatbelt profile 生成器
  - 从 `CommandSpec.Grants` 生成对应 profile 规则
  - workspace root：`(allow file-read* file-write* (subpath "<workspace>"))`
  - system paths：`(allow file-read* (subpath "/usr/bin") (subpath "/bin") (subpath "/usr/lib"))`
  - tmp：`(allow file-read* file-write* (subpath "<tmp>"))`
  - 默认 deny：network、outside-workspace write、process fork（除非 grants 声明）
- `internal/sandbox/macos/profile_test.go`：profile 生成正确性测试
- `internal/sandbox/macos/sandbox_test.go`：sandbox-exec 集成测试
  - 基础命令执行
  - 路径限制（workspace 外写入应失败）
  - 网络限制（默认 deny network）
  - Process group 管理
  - PTY I/O
- Sandbox 启动流程：
  1. 解析 `CommandSpec` 生成 Seatbelt profile
  2. 将 profile 写入临时文件
  3. 调用 `sandbox-exec -f <profile> -- <command> <args...>`
  4. 命令在 Seatbelt 约束下执行

## Acceptance

- [x] `sandbox-exec` 可用性检测（macOS 系统自带，任何 macOS >= 12 应可用）
- [x] profile 生成：workspace 内可读写，workspace 外拒绝
- [x] profile 生成：默认 deny network outbound
- [x] profile 生成：allow 声明的 system paths（简化设计：全局 `file-read*`，写路径精确限制。macOS 26 sealed volume + cryptex 使 path-based 读限制不稳定）
- [x] 基础命令在 sandbox 内执行成功（`ls`、`cat`、`grep`）
- [x] workspace 外写入被 Seatbelt 拒绝（`Operation not permitted`）
- [x] network 访问被 Seatbelt 拒绝
- [x] `ConfinementLevel()` 返回 `ConfinementKernel`
- [x] PTY I/O 在 sandbox 内正常（共用 NOP PTY manager，sandbox-exec 作用于子进程的 slave fd）
- [x] timeout + `SIGKILL` → sandbox 进程终止
- [x] 离线测试可在非 macOS 平台 skip（build tag: `darwin`）

## Evidence

Retain test output and sample generated profiles under `artifacts/m2/s2/2.2/`.

## Design notes

- `sandbox-exec` 是 macOS 系统自带工具（`/usr/bin/sandbox-exec`），无需额外安装或 entitlement。
- Seatbelt profile 使用 Scheme 语法。profile 生成器用 Go 模板构建，不手写字符串拼接。
- 不支持 `sandbox-exec` 的环境（极老的 macOS）fallback 到 NOP + `ConfinementProcess`，并 emit warning event。
- 不需要 code signing 或 App Sandbox entitlement——`sandbox-exec` 对任何可执行文件施加限制。
- profile 临时文件写入 session-owned tmp，执行后立即删除。使用 `os.CreateTemp` 并 `defer os.Remove`。
- profile 内容不包含 runtime secrets——仅路径和权限规则。
- macOS PTY 使用 `/dev/ptmx`（`posix_openpt`），由 NOP/共享的 PTY 实现提供。
