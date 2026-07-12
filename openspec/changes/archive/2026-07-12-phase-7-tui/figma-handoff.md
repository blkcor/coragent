# Figma Phase 0 handoff

Target file: [Coragent Phase 7 TUI Design Review](https://www.figma.com/design/nD1xuWUWPIyUYguzyoJ3L6)

The file was created in the `blkcor` Starter plan. This handoff is the source for building the editable design system once Phase 0 is approved and Figma MCP write capacity is available.

## Phase 0 summary

### P0.a Codebase analysis

- The product is a Go terminal application. Runtime geometry is terminal cells, not browser pixels.
- `tui/` and `cmd/coragent/` contain placeholders only. There are no existing UI components, visual tokens, fonts, screenshots, or Bubble Tea dependencies to import.
- The public SDK currently supports basic text/tool/permission/status rendering but does not yet supply rich reasoning, usage, omission, capability, preparation, or provenance facts. The OpenSpec change defines those before any screen claims them.
- The runtime must use the host terminal font. Recursive Mono is a Figma measurement reference only.

### P0.b Existing Figma file

- New blank design file, no product-owned pages, variables, components, or styles.
- No code-connected Coragent components exist.
- Starter plan means the design uses one dark semantic mode in v1. A light mode is not part of this phase.

### P0.c Subscribed-library audit

The file can access Material 3 and Figma Simple Design System. Searches found generic AI Chat, Dialog, Textarea, background variables, and spacing variables.

Decision: rebuild Coragent-specific components.

Reasons:

- Generic pixel UI components do not use terminal cell geometry.
- Their component properties and spacing tokens do not map to Bubble Tea state or Coragent event kinds.
- Material surfaces, rounded controls, and chat bubbles conflict with the selected quiet terminal direction.
- Remote components are not safely editable into the required no-color, ASCII, narrow-terminal, permission, diff, and omission states.

The library audit is still useful as a quality baseline for variable scopes and component documentation. No remote component will be detached or visually imitated.

### P0.d Locked v1 foundation plan

#### Variable collections

| Collection | Modes | Variables | Notes |
|---|---:|---:|---|
| `Coragent/Color` | `Dark` | 13 | semantic colors from `ui-design.md` |
| `Coragent/Metrics` | `Value` | 14 | cell width/row height, spacing, borders, frame widths, motion timings |
| `Coragent/Typography` | `Value` | 8 | font-size, line-height, weight references; host font remains runtime truth |

Every variable gets an explicit scope. Code syntax uses stable Go token names such as `theme.ColorCanvas` rather than CSS syntax because production is Go/Lip Gloss.

#### Text styles

1. `Coragent/Product mark`
2. `Coragent/Body`
3. `Coragent/Body strong`
4. `Coragent/Metadata`
5. `Coragent/Code and diff`

No drop-shadow effect styles are planned. Depth uses semantic background steps and borders.

#### Component sets

| Component set | Variant axes | Planned variants |
|---|---|---:|
| `Status glyph` | state | 8 |
| `Mode label` | mode/ownership, focus | 12 |
| `Context usage` | knowledge, pressure | 7 |
| `Message block` | author, state | 6 |
| `Reasoning disclosure` | state | 3 |
| `Tool row` | lifecycle, disclosure | 14 |
| `Omission marker` | reason | 5 |
| `Diff hunk` | operation, bounded | 6 |
| `Permission sheet` | origin, state | 6 |
| `Composer` | state | 5 |
| `Notice` | kind | 7 |
| `Subagent row` | state | 4 |
| `Capability row` | state | 5 |
| `Shortcut hint` | context | 5 |

The matrices stay below 30 variants. Text values use component properties. Repeated glyphs are text or vector properties, not one variant per icon.

#### Page skeleton

Eight content pages are separated by two divider entries, for ten sidebar entries total:

1. `00 Cover and decision log`
2. `01 Foundations`
3. `---`
4. `02 Components`
5. `---`
6. `03 Primary flow`
7. `04 Responsive`
8. `05 Safety and edge states`
9. `06 Prototype map`
10. `07 Developer handoff`

#### Screen frames

| Frame | Cells | Required states |
|---|---:|---|
| Reference | 120x36 | empty, streaming, tool, permission, complete |
| Wide | 160x48 | inspector, two-column permission, long diff |
| Compact | 80x24 | stacked permission, folded results, wheel-browsed history with persistent composer focus and native copy-bypass hint |
| Minimal | 60x20 | essential chrome, narrow tool and notice states |
| Too small | below 60x20 | normal warning and pending-permission fail-safe variant with no blind approval |

Additional frames: reasoning unsupported/streaming/expanded, irreversible omission, provider cutoff, content filter, subagent permission, hook block, sandbox fallback, context 80/95 percent, cancellation, step limit, startup failure, session-close failure, no-color, ASCII, CJK/wide-glyph, and control-sequence sanitization.

### P0.e Code to Figma conflicts and resolutions

| Conflict | Resolution |
|---|---|
| Current PRD forbids SDK changes but asks frontend to show unavailable facts | Add versioned frontend-neutral observability first; never put fake data in frames |
| Current edit result arrives after mutation but PRD asks for pre-apply diff | Design a prepared action with bounded diff and preimage fingerprint |
| Current permission edit can occur after the hard before-tool hook | Revised arguments re-enter before-tool hooks, are re-prepared, and get a new approval revision |
| MCP is an architecture non-goal but product brief asks to show loaded MCP | Inspector renders MCP only when truthfully reported; Phase 7 does not implement it |
| Runtime cannot control terminal font but Figma needs stable metrics | Use Recursive Mono for reference, annotate every frame in cells, test with real terminal width logic |
| Starter Figma plan has constrained variable modes | Ship one dark mode; no light/theme variants in Phase 7 |
| Generic AI Chat library exists | Rebuild because its visual and component APIs are incompatible with terminal behavior |

## Phase 1 through 4 build plan after review

### Phase 1 Checklist

- P1.a: Create the three variable collections and one mode per collection.
- P1.b: Create primitives and metrics.
- P1.c: Create semantic colors and typography values.
- P1.d: Set scopes and Go code syntax on every variable.
- P1.e: Create five text styles.
- P1.f: Validate collection, variable, scope, syntax, and style counts.

Exit: 35 variables and 5 text styles exist with no `ALL_SCOPES` leakage.

### Phase 2 Checklist

- P2.a: Create the eight content pages and two divider entries.
- P2.b: Create color, typography, grid, spacing, glyph, border, and motion documentation.
- P2.c: Screenshot and inspect each foundation section.

Exit: the file can explain the design language without opening the OpenSpec docs.

### Phase 3 Checklist

- P3.a through P3.n: Create and validate each component set in dependency order.
- Each component gets its own documentation section, variants, properties, token bindings, usage notes, metadata validation, and screenshot validation.

Exit: every screen can be composed from instances plus structural auto-layout frames.

### Phase 4 Checklist

- P4.a: Compose primary and edge-state screens.
- P4.b: Add prototype links for primary run, permission revision, cancel, mode, and scrollback flows.
- P4.c: Audit contrast, no-color meaning, naming, hardcoded paints, cell measurements, and text overflow.
- P4.d: Compare final frames with the post-review Terminal Narrative wireframes in `ui-design.md` and the runtime visual fixtures; use the original board only to recover safety-state inventory.

Exit: all planned frames have screenshots, no placeholder copy remains, and developer handoff maps components to terminal cells and observed events.

## Historical external blocker

After file creation and read-only library discovery, Figma returned: `You've reached the Figma MCP tool call limit on the Starter plan.` No `use_figma` canvas mutation was attempted after that error. The design file therefore exists but is intentionally not presented as populated.

The full build ledger preserves the state inventory if an editable Figma version is pursued later. The first local SVG/PNG board records the pre-review Quiet Instrumentation proposal and is explicitly superseded for layout and accent decisions by `ui-design.md` plus the runtime visual fixtures. The archived specs and `ui-design.md` are normative. Any future Figma reconstruction should first confirm MCP capacity, inspect the file, then begin at P1.a and rebuild components against Terminal Narrative.

## Review checkpoint

### Archive gate decision, 2026-07-12

The reviewer approved a **permanent Phase 7 waiver** for the editable-Figma gate.
The versioned SVG/PNG, revised `ui-design.md`, runtime visual fixtures, and this
handoff form the final Phase 7 design artifact. The empty Figma file is not
represented as populated, and reconstructing it later is optional follow-up
rather than an open Phase 7 requirement.

Approve or revise these four decisions before Figma mutation:

1. Terminal Narrative supersedes the original Quiet Instrumentation layout after the first runnable review: keep the low-chroma palette and semantic states, but use Claude Code-style top-aligned conversation flow, one-cell markers, indented results, a hairline composer, and a full-width bottom permission panel.
2. One dark mode is sufficient for Phase 7.
3. Skills and MCP are conditional display categories, not Phase 7 runtimes.
4. Bypass requires guarded entry and is excluded from casual mode cycling.
