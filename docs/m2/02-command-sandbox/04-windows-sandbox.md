# S2.4 Windows Sandbox: NOP + ConPTY

**Status:** accepted
**Prerequisite:** [S2.1 accepted](01-sandbox-interface.md)

## Goal

Windows 平台没有 unprivileged kernel 级 sandbox 机制（AppContainer 需要 UWP/package identity），因此使用 NOP sandbox 提供进程组级别的管理（`ConfinementProcess`），PTY 方面通过 ConPTY API 提供完整的伪终端支持。

## Deliverables

- `internal/sandbox/windows/sandbox.go`：复用 NOP 实现的 `Sandbox` 接口
  - `ConfinementLevel()` 返回 `ConfinementProcess`
  - 进程组管理（`CreateJobObject` 代替 Unix process group）
  - 环境变量最小化
  - timeout 后 `TerminateJobObject` 终止整个 job
- `internal/sandbox/windows/pty.go`：ConPTY `PTYManager` 实现
  - Windows 10 >= 1809 (build 17763)：使用 `CreatePseudoConsole` API
  - 旧版 Windows (< 1809)：降级为 pipe（`CreatePipe`）
  - PTY resize：使用 `ResizePseudoConsole`
  - I/O：使用 overlapped I/O 读取 ConPTY master 端
- `internal/sandbox/windows/version.go`：Windows 版本检测
  - 读取 `RtlGetVersion` 或 kernel32.dll 版本信息
  - 缓存检测结果（进程生命周期内不变）
- `internal/sandbox/windows/sandbox_test.go`：集成测试
  - 基础命令执行
  - ConPTY I/O
  - Process group 管理（job object）
  - timeout + TerminateJobObject
  - Pipe fallback on < 1809
- Build tag: `windows`

## Windows PTY 策略

```
启动时检测 Windows 版本
    │
    ├── build >= 17763 (1809+)
    │     └── ConPTY: CreatePseudoConsole + overlapped I/O
    │
    └── build < 17763
          └── Pipe fallback: CreatePipe (stdin/stdout/stderr 直接用 pipe)
```

ConPTY 和 Unix PTY 在同一 `PTYManager` 接口下统一：
- `Allocate()` → ConPTY 创建 master/slave handle pair
- `Resize()` → `ResizePseudoConsole`
- `ReadLoop()` → overlapped `ReadFile` on master handle

Pipe fallback 场景下 `Resize()` 为 no-op（pipe 没有行列概念），`Allocate()` 返回标准 pipe handle pair。

## Acceptance

- [x] NOP sandbox 可启动基础命令（`cmd /c echo hello`、`powershell -Command Write-Output hello`）
- [x] ConPTY 在 Windows >= 1809 上正常分配、I/O、resize
- [x] 旧版 Windows (< 1809) 正确降级为 pipe，命令输出可捕获
- [x] 版本检测精确到 build number（`RtlGetVersion`），结果缓存
- [x] `CreateJobObject` → 子进程被 job 管理，`TerminateJobObject` 终止所有子进程
- [x] timeout 后 `TerminateJobObject` 正确终止整个 job
- [x] `ConfinementLevel()` 返回 `ConfinementProcess`
- [x] PTY I/O 在 ConPTY 和 pipe 两种路径下均正常
- [x] 离线测试在非 Windows 平台 skip（build tag: `windows`）

## Evidence

Retain test output under `artifacts/m2/s2/2.4/`.

## Design notes

- Windows 没有 unprivileged kernel sandbox（AppContainer 需要 package identity + manifest，不适用于通用命令行工具，Sandboxie/Windows Sandbox 需要额外组件）。因此 Windows 停留在 `ConfinementProcess` 级别——环境清理 + 进程组管理，但不强制执行路径/网络限制。
- ConPTY API 从 Windows 10 1809 (build 17763) 开始可用，对应 kernel32.dll 中的 `CreatePseudoConsole`、`ResizePseudoConsole`、`ClosePseudoConsole`。使用 `syscall.NewLazyDLL` 动态加载，避免静态链接导致的旧版 Windows 兼容性问题。
- job object 用于进程组管理（等价于 Unix process group）：`CreateJobObject` + `AssignProcessToJobObject`，timeout 时 `TerminateJobObject` 强制终止所有成员进程（包括子进程的子进程）。
- 环境变量清理在 `CreateProcess` 时显式传入最小集合，不继承 host 环境。
- Pipe fallback 路径不提供 PTY resize 能力——`PTYManager.Resize()` 在该路径上为 no-op。调用方可通过 `PTYManager.Capabilities()` 查询（如果需要暴露此信息）。
- 和 macOS/Linux PTY 实现共享同一 `PTYManager` 接口，调用方不感知 ConPTY vs posix_openpt 的差异。
