# S2.3 Linux Sandbox: Landlock + seccomp Backend

**Status:** accepted (2026-08-08)
**Prerequisite:** [S2.1 accepted](01-sandbox-interface.md)

## Goal

实现 Linux 平台 `Sandbox` 接口，使用 Landlock (filesystem) + seccomp (syscall filter) 提供 kernel 级 (`ConfinementKernel`) 进程隔离。两个机制都是 unprivileged——无需 root 或 CAP_SYS_ADMIN。

## Deliverables

- `internal/sandbox/linux/sandbox.go`：`Sandbox` 接口的 Landlock + seccomp 实现
- `internal/sandbox/linux/landlock.go`：Landlock ruleset 构建
  - 使用 `golang.org/x/sys/unix` 的 Landlock 系统调用
  - 从 `CommandSpec.Grants` 构建允许的文件访问规则
  - workspace：`LANDLOCK_ACCESS_FS_READ_FILE | LANDLOCK_ACCESS_FS_WRITE_FILE`
  - tmp：同上
  - 默认 deny：所有不在 grant 中的路径
- `internal/sandbox/linux/seccomp.go`：seccomp filter 构建
  - 使用 `golang.org/x/sys/unix` 的 seccomp BPF
  - 默认 deny：network socket、mount、chmod 等危险 syscall
  - allow：read、write、exit、fstat 等基本 syscall
  - 如果 grant 声明了 network，allow socket 系列 syscall
- `internal/sandbox/linux/sandbox_test.go`：集成测试
  - 基础命令执行
  - 路径限制（workspace 外写入应失败）
  - 网络限制
  - Process group 管理
  - PTY I/O
- 可用性检测：
  - Landlock: kernel >= 5.13，以非零 `handled_access_fs` 试建 ruleset 探活
    （mainline 无 `/proc/sys/kernel/landlock/abi`；VERSION 查询用 flags=1）
  - seccomp: kernel >= 3.17，几乎总是可用
  - 不可用时 fallback 到 NOP + warning event

## Acceptance

- [x] Landlock ruleset 构建正确：workspace 内读写放行，workspace 外拒绝（实测：workspace 外写 `/root` 被拒）
- [x] seccomp filter 构建正确：基本 syscall 放行，危险 syscall 拒绝（新增 classic-BPF 语义回归测试，amd64/arm64 双架构交叉编译）
- [x] 基础命令在 sandbox 内执行成功（echo/cat/sh，真实内核 6.12.76-linuxkit, Landlock ABI v6）
- [x] workspace 外写入被拒绝
- [x] network 访问默认被拒绝（socket syscall 被 seccomp 拦截；grant 声明 network 时放行）
- [x] `ConfinementLevel()` 返回 `ConfinementKernel`
- [x] kernel 不支持时检测并 fail-closed（`Start` 返回 typed error；fallback 到 NOP + warning 由 S2.7 command tool 负责选 backend，与 macOS 后端同一契约）
- [x] PTY I/O 在 sandbox 内正常（TestPTYBasic，Linux /dev/ptmx）
- [x] timeout + `SIGKILL` — sandbox 进程正确终止
- [x] 离线测试在非 Linux 平台 skip（build tag: `linux`）

## Evidence

Retain test output and sample Landlock/seccomp configurations under `artifacts/m2/s2/2.3/`.

## Design notes

- Landlock 和 seccomp 使用纯 Go 系统调用（`unix.LandlockCreateRuleset`、`unix.Seccomp`），不依赖外部 binary。
- Landlock 在 `fork/exec` 之间应用：子进程在 exec 前 self-restrict。Go 的 `syscall.RawSyscall` 或 `unix` 包封装调用。
- seccomp filter 在 Landlock 之后加载。两个机制独立，任一失败 = 整体 sandbox init 失败。
- Linux PTY 使用 `posix_openpt`（`/dev/ptmx`），与 macOS 共享 PTY 实现代码路径。
- Landlock ABI 版本检查在 sandbox 初始化时做一次，结果缓存。`/sys/kernel/security/lsm` 是另一个可选的检测点。
- 不做 seccomp BPF 手写——使用 `golang.org/x/sys/unix` 的 seccomp filter builder 或生成 BPF 指令。
- 如果 Landlock 不可用但 seccomp 可用，是否单独使用 seccomp？当前设计：两者必须都可用才返回 `ConfinementKernel`，缺少任一则 fallback。后续可加 seccomp-only 级别 `ConfinementPartial`。
