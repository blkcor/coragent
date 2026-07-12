# Phase 7 TUI 设计评审指南

## 本轮评审目标

本轮只冻结产品、交互、视觉、SDK 契约与实施边界，不开始编码。评审通过后，`phase-7-tui` 应当可以直接进入 OpenSpec apply，不再补一轮方向性设计。

当前方案把 Phase 7 定义为两件同时成立的产品能力：

1. 对最终用户，它是可长期使用、可解释、可安全审批的终端前端。
2. 对 SDK 使用者，它证明任何前端只依赖 `pkg/agent` 就能获得同等真实信息，不需要绕过公共边界读取 `internal/*`。

## 建议评审顺序

1. 先读 [proposal.md](proposal.md)，确认范围、非目标和公共能力变化。
2. 再读 [design.md](design.md)，确认事件契约、执行安全、状态架构和迁移策略。
3. 查看 [assets/phase-7-reference.png](assets/phase-7-reference.png) 与 [ui-design.md](ui-design.md)，确认视觉方向、组件状态、响应式布局和交互细节。
4. 查看 [figma-handoff.md](figma-handoff.md)，确认 Figma Phase 0 决策、页面结构、组件变量和后续建图计划。
5. 按能力检查 `specs/*/spec.md`，重点看 `tui-frontend`、`session-observability`、`action-preview`、`permission` 与 `hooks`。
6. 最后读 [tasks.md](tasks.md)，确认实施顺序、拆分粒度和验收闭环。

## 必须明确批准的九个决策

| 编号 | 当前建议 | 为什么 | 如果不批准，需要改什么 |
|---|---|---|---|
| D1 范围 | Phase 7 允许增加前端无关、向后兼容的公共 SDK 契约 | 现有 `RunEvent` 无法真实表达 reasoning summary、持续 context usage、最终预览、结构化省略和子代理来源 | 若坚持零 SDK 变化，就必须删掉这些 UI 承诺，不能用字符串猜测或由 TUI 私自读取内部状态 |
| D2 Skills/MCP | 本阶段不实现 skills runtime 或 MCP client，只显示真实 capability reporter 提供的类别 | 当前架构明确把 MCP 排除在 v1 外，伪造 `0 loaded` 会误导用户 | 若要纳入，先单独批准架构变更、运行时、配置、安全和测试范围，再修改本 change |
| D3 Reasoning | 只显示 provider 明确标记为可展示的 reasoning summary | 能给用户进度解释，同时不请求、推断、持久化或泄露隐藏 chain-of-thought | 若要展示更多，必须定义新的安全来源，不能把普通 assistant 文本或耗时伪装成 reasoning |
| D4 Action preview | 预览必须在 hard hook 之后，由 executor 对真实候选动作生成 | TUI 无法仅凭参数知道旧文件、hook 替换、sandbox grants 或竞态 | 若由 TUI 自算 diff，会破坏公共边界并产生批准内容与实际执行不一致的风险 |
| D5 参数修改 | `Ctrl+S` 只提交 revision；重新校验、hard hook 与 preparation 全部成功后才产生新的 permission | 人工修改发生在首次 hard gate 之后，直接执行会形成安全旁路 | 若保留一次提交即批准，必须接受无法证明 hard hook 覆盖修订参数的风险，不建议 |
| D6 Bypass | `Shift+Tab` 只循环 default、auto-accept-edits、plan；bypass 用 `Ctrl+B` 加确认 | 高频模式切换不应无意进入最高风险状态 | 若要把 bypass 放回循环，需要同步修改键盘、警示、测试和安全验收标准 |
| D7 信息架构 | 默认采用 Claude Code 式单列 terminal narrative，user、assistant、tool 共用一字符状态标记，inspector 按需打开，不设常驻侧栏 | 终端宽度有限，用户的主要任务是沿时间顺序理解和审批工作，固定 telemetry 列会与正文争抢空间 | 若要 dashboard 或常驻树，需要重新设计 60、80 列和 permission 优先级 |
| D8 视觉方向 | 采用 Terminal Narrative：低彩深色、陶土暖色信号、宿主终端字体、无渐变、无玻璃、无居中 dashboard 卡片 | 它保留 Claude Code 的自然阅读流，同时让 Coragent 的 mode、sandbox 和 permission 事实仍然可见 | 若换风格，请同时给出色彩、状态区分、无色模式和窄终端的替代规则 |
| D9 Figma gate | 永久接受版本化 SVG/PNG、修订后的 UI design、运行时 visual fixtures 与完整 handoff 作为 Phase 7 最终设计资产 | Starter MCP limit 是外部阻塞，不能把空文件冒充成已完成设计；首版 SVG/PNG 也已被用户反馈推翻，不能继续作为当前布局基线 | 2026-07-12 归档决定为永久豁免；额度恢复后可按 Terminal Narrative 自愿重建 editable Figma，但不再是 Phase 7 open task |

最终归档批准当前九项选择，并接受 `ui-design.md`、版本化本地资产与运行时 visual fixtures 作为 Phase 7 最终评审基线。editable Figma 可选后续重建，不再保留为 Phase 7 完成任务。它们共同保持一个原则：界面只展示 harness 能证明为真的事实。

兼容性说明：所有旧 API signature 和文档内合法用法保持兼容。有两项 fail-safe 收紧需要在 D1 一并批准：原本就声明只能在 turn 之间调用的 string mode setter，在 mid-run 误用时从“无保证地改变状态”改为稳定报错；legacy edited approval 若被重新执行的 hard hook 改成另一组参数，必须展示新的 permission request，不能让旧批准授权未展示参数。

## 跨层真实性检查

| UI 显示 | 唯一可信来源 | 不支持时的降级 |
|---|---|---|
| reasoning summary | rich provider 明确的 display-safe summary | 只显示活动状态，不创建空 reasoning 区块 |
| context usage | 每轮请求前的明确估算，或 provider usage | 标 `est`；window 未知时不显示虚假百分比 |
| file diff | hard hook 后的 prepared action | legacy handler 只显示安全的参数摘要，不伪造 diff |
| result omission | typed omission event | 本地折叠仍可展开，不能冒充数据已丢失 |
| tools/hooks | session effective descriptor | 只列实际可执行或实际注册的能力，描述不会扩权 |
| skills/MCP | custom/future capability reporter | 未报告时正常界面不显示 loaded 数量 |
| sandbox | session descriptor 与 per-call grants | fallback 显示为安全告警，不冒充 OS confinement |
| externally owned mode | session descriptor ownership | 显示 `EXTERNAL`/`UNSUPPORTED` 并禁用模式快捷键，不伪造四种 engine mode |
| subagent | root-visible stable lifecycle provenance | 不复制 child 原始文本、reasoning、tool 或 hook stream |

## 核心安全流评审

请逐步确认以下顺序不可打乱：

```text
model arguments
  -> schema validation
  -> hard before-tool hooks
  -> validate effective arguments
  -> prepare candidate and preview
  -> permission request revision N
  -> optional revise_arguments
  -> reject malformed/schema-invalid reply without consuming request N
  -> accept a schema-valid revision and close request N exactly once
  -> hard before-tool hooks again
     -> block/failure: terminate without fabricating another prompt
     -> allow: prepare candidate and preview again
        -> preparation failure: terminate without another prompt
        -> success: permission request revision N+1
  -> explicit allow or deny
  -> sandbox routing for command tools
  -> no-follow parent/target identity and preimage validation
  -> stage + fsync candidate, revalidate, then atomically replace
  -> Tool.Execute for non-prepared handlers
  -> hard post-tool hooks
  -> bounded result and typed omission
```

评审通过标准：任何被执行的 mutation 都必须对应用户最后看到并批准的 preview revision；文件、inode、symlink、hard-link count 或 parent identity 在预览后发生变化时必须 fail closed，不能用“内容刚好相同”绕过，也不能让一个展示路径的批准静默修改其他 hard-link alias。

## 重点界面状态

以下状态都已经进入 UI spec、视觉清单或测试任务，评审时不应只看正常成功路径：

- 首次启动、空会话、streaming text、完成 Markdown、reasoning unsupported/streaming/completed。
- tool proposed、prepared、awaiting permission、executing、success、error、cancelled、hook blocked。
- built-in create/modify diff、custom/future delete preview、binary metadata、preview bounded、stale preview、output omitted、provider cutoff、content filter、redacted；本阶段不新增 delete tool。
- permission allow/deny/remember、submitting/accepted/rejected、argument editor invalid/re-preparing/new request、sandbox grants、child permission、caller-owned legacy prompt。
- context estimated/provider/unknown window/80 percent/95 percent、sandbox fallback、hook notice、subagent completed/failed/cancelled/reached-step-limit、depth-limit task error。
- history browsing/unread、cancel、clean quit、startup error、close error、no-color、ASCII、reduced motion、CJK/wide glyph、terminal too small。

## 视觉验收基线

- 设计名：`Terminal Narrative`。
- 主结构：一行 identity、顶对齐单列 transcript、固定在底部的上下 hairline composer、一行 mode/sandbox/model/context 状态与动态快捷键提示；空行只补在短 transcript 之后。
- 标志性元素：一字符执行标记与 `└` 结果分支，不设固定宽度 telemetry 状态列；短对话前不补空行。
- 主色：canvas `#0C0D0C`，surface `#131512`，primary `#E7E9E3`，muted `#90988B`，accent `#D97757`。
- 语义色：success `#7EBC77`，danger `#E17A72`，info `#8FA9A1`。
- 目标尺寸：160x48、120x36、80x24、60x20；低于 60x20 时切换为安全视图，只保留 deny/cancel/quit 与 resize 提示，禁止无法完整审阅内容时盲批。
- 动画：仅 active rail 120 ms 与 cursor 600 ms；idle 停止 tick；reduced motion 使用静态反馈。
- 字体：运行时完全使用用户终端字体；Figma 的 Recursive Mono 仅用于 8x18 px cell-grid 参考。

## Figma 当前状态

设计文件已创建：[Phase 7 TUI Figma file](https://www.figma.com/design/nD1xuWUWPIyUYguzyoJ3L6)。

Figma Starter 计划随后返回 MCP tool call limit，因此没有在额度错误后继续写画布，也不把空文件描述成已完成设计。当前可完整评审的视觉源是修订后的 UI design、运行时 visual fixtures 和 Figma handoff，其中 specs 与 UI design 是规范性来源；首版 reference board 只保留状态清单价值，不再代表当前排版。额度恢复后，按 `figma-handoff.md` 从 P1.a 以 Terminal Narrative 建变量、组件和 frame。

## 实施与验收边界

`tasks.md` 按依赖拆成三条可独立审查的交付线：

1. 公共 observability、provider、context、bootstrap 与兼容投影。
2. prepared action、permission revision、hard hook、sandbox grant 与 omission。
3. fake `SessionPort` 驱动的 TUI reducer、视觉层、公共 SDK adapter 与 binary。

最终 gate 包括 offline fake tests、legacy compatibility、race/cancellation、golden renders、PTY、终端矩阵、全量 Go 验证、import boundary 和 strict OpenSpec。设计评审之前没有修改产品代码。

## 建议回复格式

可以直接在评审后回复：

```text
D1 approve/change: ...
D2 approve/change: ...
D3 approve/change: ...
D4 approve/change: ...
D5 approve/change: ...
D6 approve/change: ...
D7 approve/change: ...
D8 approve/change: ...
D9 approve/change: ...
其他逐条意见: ...
```

九项都批准后，可以直接说“按照 phase-7-tui spec 开始执行”，届时进入 coding，不再重新做方向性规划。
