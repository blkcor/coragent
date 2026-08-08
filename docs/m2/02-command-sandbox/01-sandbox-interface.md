# S2.1 Sandbox Runtime Interface + NOP Implementation

**Status:** pending
**Prerequisite:** M2 S1 merged

## Goal

定义平台无关的 `Sandbox` 接口和 `PTYManager` 接口，提供 NOP 实现用于测试和 Windows 平台（以及不支持 kernel 级 sandbox 的环境）。建立 `ConfinementLevel` 分级机制，让 command 工具在所有平台可用但诚实标注隔离级别。

## Deliverables

- `internal/sandbox/sandbox.go`：`Sandbox` 接口定义
  - `Start(ctx context.Context, spec CommandSpec) (Process, error)`
  - `ConfinementLevel() ConfinementLevel`
- `internal/sandbox/spec.go`：`CommandSpec`、`ProcessResult`、`Grants` 类型定义
- `internal/sandbox/process.go`：`Process` 接口定义
  - `PID() int`
  - `Done() <-chan struct{}`
  - `Result() ProcessResult`
  - `Signal(os.Signal) error`
  - `ResizePTY(rows, cols int) error`
- `internal/sandbox/confinement.go`：`ConfinementLevel` 枚举
  - `ConfinementNone` — NOP（无任何隔离）
  - `ConfinementProcess` — 进程组 + 环境清理
  - `ConfinementKernel` — OS kernel 级强制隔离
- `internal/sandbox/nop/`：NOP 实现
  - 使用 `os/exec` 启动进程，设置 process group
  - 最小化环境变量（不转发 ambient credentials）
  - PTY 分配（Unix: `posix_openpt`；Windows ≥ 1809: ConPTY；旧版 Windows: pipe fallback）
  - stdout/stderr 合并、bounded buffering
  - timeout 后 `SIGKILL` 整个 process group
  - `ConfinementLevel()` 返回 `ConfinementProcess`
- `internal/sandbox/pty.go`：`PTYManager` 接口定义
  - `Allocate() (master *os.File, slave *os.File, err error)`
  - `Resize(master *os.File, rows, cols int) error`
  - `ReadLoop(ctx context.Context, master *os.File, buf io.Writer, maxBytes int64) error`
- 离线测试：NOP sandbox 基本流程、process group 清理、PTY I/O

## Acceptance

- [ ] `Sandbox`、`Process`、`PTYManager` 接口定义清晰，每个方法有明确的语义和错误约定
- [ ] `CommandSpec` 覆盖所有 command 执行所需参数（command、args、cwd、env、timeout、max output、pty flag、grants）
- [ ] `ConfinementLevel` 三种取值语义明确，NOP 返回 `ConfinementProcess`
- [ ] NOP sandbox 可启动简单命令（`echo hello`）并捕获输出
- [ ] NOP sandbox timeout 后终止进程组（包括子进程）
- [ ] NOP sandbox PTY I/O 正常工作（Unix 平台）
- [ ] context 取消 → 进程组收到 `SIGKILL`
- [ ] `Grants` 结构支持声明允许的文件路径（为后续 platform sandbox 使用）
- [ ] 离线测试覆盖上述所有场景

## Evidence

Retain test output under `artifacts/m2/s2/2.1/`.

## Design notes

- `CommandSpec.Env` 是 **显式最小集合**，不是继承 host 环境再过滤。调用方负责构建。
- `Grants` 在 NOP 阶段仅定义结构，不被 NOP 实现 enforce；macOS/Linux sandbox 才真正执行限制。
- PTY 分配策略：Unix 使用 `posix_openpt`（Go `creack/pty` 库封装）；Windows 10 1809+ 使用 ConPTY API；旧版 Windows 降级为 pipe。`PTYManager` 接口统一这三种实现路径，调用方不感知平台差异。
- NOP 不是"安全 sandbox"——它提供进程组管理和 I/O 控制，但无路径隔离。调用方必须通过 `ConfinementLevel()` 知道当前隔离级别并据此决策（如 NOP 平台可能拒绝执行某些 command 类别）。
