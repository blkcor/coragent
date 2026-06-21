# Coragent — Roadmap

Phased delivery. Each phase has a deep PRD in `docs/prd/`. Read
`architecture.md` first — it is the canonical spec all phases obey.

## Phase order & dependencies

```
0 Foundations
      └─► 1 Agent Loop ⭐
              └─► 2 Tools & Executor
                      ├─► 3 Permission Engine
                      │       └─► 4 Hooks Engine
                      │               └─► 5 Sandbox
                      ├─► 6 Subagents
                      └─► 7 TUI Frontend
```

Each phase is runnable on the one before it. The loop (Phase 1) is the heart and
gets the deepest PRD.

## Overview

| # | PRD | Delivers | Depends on |
|---|-----|----------|------------|
| 0 | [phase-0-foundations](prd/prd-phase-0-foundations.md) | repo layout, core types, Provider interface + OpenAI-compat client (stream + tool-calls), config loader | — |
| 1 | [phase-1-agent-loop](prd/prd-phase-1-agent-loop.md) ⭐ | single→multi-turn tool loop, event stream, context manager basics | 0 |
| 2 | [phase-2-tools-executor](prd/prd-phase-2-tools-executor.md) | tool registry + middleware executor; read/write/edit/shell/grep/glob | 1 |
| 3 | [phase-3-permission](prd/prd-phase-3-permission.md) | allow/deny/ask engine, modes, remembered rules (soft, human-in-loop) | 2 |
| 4 | [phase-4-hooks](prd/prd-phase-4-hooks.md) | hard gates — external command + Go-func hooks, matchers, lifecycle events | 2,3 |
| 5 | [phase-5-sandbox](prd/prd-phase-5-sandbox.md) | `sandbox-exec` Seatbelt backend, policy derivation, profile generation | 2,4 |
| 6 | [phase-6-subagents](prd/prd-phase-6-subagents.md) | subagent orchestrator + `task` tool, isolated context, result-only return | 1,2 |
| 7 | [phase-7-tui](prd/prd-phase-7-tui.md) | Bubble Tea frontend on the event stream; permission modals, tool cards, diffs | 1–6 |

## Milestones

- **M1 — "It talks":** Phases 0+1. A fake/real provider drives a multi-turn loop;
  events stream to a trivial stdout frontend. No tools yet.
- **M2 — "It acts":** Phases 2+3. Agent reads/edits files and runs commands with
  human-in-the-loop permission.
- **M3 — "It's safe":** Phases 4+5. Hard hooks + OS sandbox enforced on every call.
- **M4 — "It scales":** Phase 6. Subagents for focused/parallel work.
- **M5 — "It ships":** Phase 7. Full TUI daily-driver.

## Definition of done

See `architecture.md` §9 — applies uniformly to every phase.
