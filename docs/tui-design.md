# Coragent TUI: Run Ledger

## Visual theme

Run Ledger treats an agent session as an append-only operating record. The
canvas is quiet and nearly black, execution is organized by stable `TASK` and
`STEP` ordinals, and semantic color is reserved for state rather than ornament.

## Palette

| Role | Value | Use |
| --- | --- | --- |
| Canvas | `#090C0A` | terminal background |
| Surface | `#121711` | user task entries |
| Elevated | `#192018` | review and inspector surfaces |
| Border | `#2C352A` | rules and viewport structure |
| Text | `#E8EBDD` | primary copy |
| Muted | `#8C9484` | metadata and inactive state |
| Accent | `#C8D45A` | current task, focus, pending work |
| Success | `#82B67E` | completed receipts |
| Warning | `#D6AA58` | recoverable attention |
| Danger | `#E0766E` | failure and bypass state |
| Info | `#91AAA0` | secondary runtime information |

## Typography and hierarchy

Coragent uses the terminal's monospace face. Uppercase micro-labels identify
structure, not chat roles: `TASK 02`, `STEP 04`, `REVIEW`, and `INSPECT`.
Ordinals are two digits so streaming state changes cannot move adjacent text.

## Components

- Header: product, stable task number, compressed workspace, mode, live status.
- User entry: one surface row headed by its stable task number.
- Tool receipt: one collapsed row by default, with step number, outcome, and
  duration. Successful output and preview details live in the inspector.
- Review: an elevated decision surface. Unsupported preview plumbing never
  appears in the primary timeline.
- Composer: a compact ruled entry area anchored above the metadata footer.
- Help: an explicit overlay. The normal screen has no permanent key-hint row.

## Layout

The terminal uses four supported geometries: `60x20`, `80x24`, `120x36`, and
`160x48`. Header, footer, and minimum composer remain fixed; every other row is
given to the transcript. Wider layouts reveal workspace, model, and metadata in
that order. Labels are dropped whole when they do not fit.

## Depth

Depth comes only from background steps: canvas, surface, then elevated. There
are no decorative shadows or card grids. Hairline rules separate the composer
and focused review surfaces.

## Guardrails

- Keep successful tool calls to one primary-timeline row.
- Keep preview capability errors in the inspector.
- Never renumber a task or tool step after it appears.
- Do not add a permanent shortcut legend.
- Preserve exact cell geometry in every color mode.
- Preserve ASCII, `NO_COLOR`, and reduced-motion fallbacks.
- Use warning and danger only for states that require attention.

## Responsive behavior

At `60x20`, workspace and model metadata yield before task, mode, status, and
sandbox safety labels. Permission review remains deny-capable below the minimum
supported geometry. At wider sizes, prose stays capped while operational rows
can use the available width.

## Agent prompt guide

When extending the TUI, use canvas `#090C0A`, surface `#121711`, elevated
`#192018`, border `#2C352A`, text `#E8EBDD`, muted `#8C9484`, accent `#C8D45A`,
success `#82B67E`, warning `#D6AA58`, danger `#E0766E`, and info `#91AAA0`.
Build terminal-cell geometry with Lip Gloss, keep state labels width-stable,
and put detailed diagnostics behind the inspector.
