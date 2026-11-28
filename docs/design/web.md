---
version: alpha
name: smithy-web
description: Slab on the web. Browser view of the same workspace as the TUI, on Sephiro's Temper theme.
# Token names match docs/design/tui.md. Values: steel, from web/src/theme/temper.css (--t-* layer).
colors:
  light:
    surface: "#f7f7f6"   # --t-bg
    raised:  "#efefee"   # --t-side, --t-card #ffffff
    focus:   "#f0f0ef"   # --t-raised
    border:  "rgb(23 23 23 / 0.09)"
    strong:  "#18181a"
    text:    "#52525a"
    muted:   "#6f6f78"
    accent:  "#c2410c"
    ok:      "#15803d"
    warn:    "#a16207"
    err:     "#dc2626"
  dark:
    surface: "#141415"
    raised:  "#0f0f10"   # card #19191b
    focus:   "#232325"
    border:  "rgb(237 237 237 / 0.08)"
    strong:  "#ededed"
    text:    "#a1a1a6"
    muted:   "#85858b"
    accent:  "#f2762e"
    ok:      "#4cc38a"
    warn:    "#e3b341"
    err:     "#e5484d"
typography:
  sans: {fontFamily: "Geist, ui-sans-serif, -apple-system, system-ui", fontSize: 14px, lineHeight: 1.5}
  mono: {fontFamily: "Geist Mono, ui-monospace, SF Mono, Menlo", fontSize: 13px, lineHeight: 1.5}
rounded: {xs: 0, sm: 0, md: 0, lg: 0, full: 0}
spacing: {gutter: 16px, indent: 24px, block-gap: 16px, chip-gap: 8px, sidebar: 320px}
motion: {spring: "bounce 0, ~180ms", reduced: "prefers-reduced-motion or ui.reduce_motion: none"}
components:
  tab:            {bg: "{colors.raised}", fg: "{colors.muted}"}
  tab-hover:      {fg: "{colors.strong}"}
  tab-active:     {bg: "{colors.accent}", fg: "{colors.surface}", weight: 600}
  tab-focus:      {outline: "2px {colors.accent}"}
  tab-disabled:   {fg: "{colors.muted}", opacity: 0.5}
  input:          {bg: "{colors.focus}", rail: "3px {colors.accent}"}
  input-disabled: {rail: "{colors.border}"}
  row-focus:      {bg: "{colors.focus}", rail: "3px {colors.accent}"}
---

# smithy web design: Slab

## Overview

The web app (`web/`, Preact; built into `internal/web/dist/app`) shows the same sessions, asks, links
and settings as the TUI and follows the same rules as [tui.md](tui.md): flat, square, hierarchy from
tiers, the theme accent for focus/you/active, signals only for state, one status vocabulary.
Components come from Sephiro (`@zovaris/sephiro`); fixes go to Sephiro by PR, not into `web/`.

## Colors

Root attributes, one switch each:
- `data-sephiro-theme="temper"`.
- `data-accent="<theme>"` = `ui.theme` (frost, plum, moss, dune, dusk, rose; steel sets none), read and
  saved through the daemon (`/v1/settings/ui.theme`, `web/src/lib/theme.ts`), so web and TUI match.
- `data-mode="light|dark"`, else `prefers-color-scheme`.

`temper.css` maps `--t-*` (smithy roles) onto `--sph-*` (Sephiro tokens). Components read only
`--sph-*`. Theme blocks copy hex from `internal/tui/kit/theme.go`; change both in one PR.

| tui.md token | `--t-*` | `--sph-*` |
|---|---|---|
| surface | `--t-bg` | `--sph-bg` |
| raised | `--t-side`, `--t-card` | `--sph-surface` |
| focus | `--t-raised` | `--sph-raised` |
| overlay | (raised + mix) | `--sph-surface-elevated` |
| border | `--t-line` | `--sph-border` |
| strong / text / muted | `--t-text` / `--t-text-2` / `--t-text-3` | `--sph-text` / `--sph-text-muted` |
| accent | `--t-ember` | `--sph-accent`, `--sph-focus` |
| ok / warn / err | `--t-ok` / `--t-wait` / `--t-fail` | `--sph-success` / `--sph-warning` / `--sph-danger` |
| info | missing: add `--t-info` | missing |

Known gaps: steel's web base is a graphite neutral, not TUI steel; no info token yet. Text on an
accent fill uses `--sph-on-accent`. Body text ≥ 4.5:1 on every tier, in every theme and mode.

## Typography

Geist and Geist Mono. Transcript, tool rows, shell and chips in mono; settings and dialogs in sans.
Weight 600 for titles, user text and the active tab. Sentence case, no uppercase or tracking.

## Layout

Same structure as the TUI. Sidebar (320px) from 1024px wide; below it the sidebar moves behind a
header button. 16px gutter on phones, no horizontal scroll, `100dvh` for full height.

Desktop:
```
+------------------------------------------------------------------+------------------+
| [◆ smithy] v0.26 · skills                    tend ▪  ⠹ working   | fix the flaky    |
| [main] [⠹ fix-ci] [? review] [+]                                 |                  |
|                                                                  | Context          |
| ▌ fix the flaky test in tests/run.sh                      14:01  | ██████▒▒▒▒  46%  |
|   builder  sonnet · high                                         | Spend            |
|    ▪ Read  tests/run.sh                              ✓ 53 lines  | today     $0.84  |
|   [$] bash tests/run.sh                          exit 0 · 1.8s   | Links            |
|                                                                  | ▪ #276 fix ...   |
| ▌ Questions: Keep the retry or remove it?  (1 / 2 / your own)    |   ci ⠹ 4/6       |
| ▌ ›                                                              | Queue 1 rerun    |
|   [builder] [sonnet] [effort high] [2 queued]          12.4k tok |                  |
+------------------------------------------------------------------+------------------+
```
Phone: header, tab chips scroll sideways, transcript, ask panel and input docked to the bottom.

## Elevation & Depth

Tiers, not shadows. Dialogs: raised body, full-width accent title bar, a 4px offset shade in the
darkest ramp step right and below, and a dimmed backdrop. `--sph-shadow-popover` stays for popovers
only. No `backdrop-filter`.

## Shapes

All radii 0. The rail is a 3px accent inline-start bar on the user message, the input and the focused
row; it marks a state (you, focus), so it is not decoration.

## Motion

Dialog open/close and value changes use a spring with no bounce, ~180ms, opacity and transform only;
never `transition: all`. Spinner is the braille set at 80ms. `prefers-reduced-motion` or
`ui.reduce_motion`: no motion, a still spinner frame plus the word "working". Motion never delays input.

## Voice & Content

English, terse, same strings as the TUI: `done:`, `exit 0 · 1.8s`, `ci ✓ · 1 review`,
`→ Asked 1 question`, "earlier runs, agent not recorded". No em dashes in UI copy.

## Components

Status vocabulary as in tui.md: square mark, `ci ✓` / `ci ✗` / `ci ⠹ 4/6`, warn `?` chip, spinner. No
circles. Every mark has text or an `aria-label`; live updates use a polite live region.

| Surface | Sephiro | States |
|---|---|---|
| Settings (`/config`) | `SettingsList`, `Select`, `Toggle`, `SegmentedControl` | row default / hover / focus (focus bg, rail) / disabled; edited `•` accent; theme row shows accent swatches |
| Palette | `CommandPalette` | open, item focus = focus bg + rail |
| Usage, doctor, ask | `Dialog` | open, closing; check rows ok / warn / failed |
| Batch, queue | `TaskBoard` | running spinner, waiting `?`, done, failed |
| Period and view switch | `SegmentedControl` | active = accent fill, on-accent text |
| Tabs, chips, rail, panels, charts | local `web/src/components` (`rail`, `panel`, `decision`, `charts`, `links`) | default, hover, focus-visible (2px accent ring), active, disabled, running, failed |

Every interactive element is a `<button>` or `<a>`, with hover, focus-visible, active and disabled.
Data views have loading, empty and error states.

## Do's and Don'ts

| Do | Don't |
|---|---|
| Colours only from `--sph-*`; add a token first | Hex literals in components |
| Keep `temper.css` equal to `theme.go` | Drift a palette on one surface |
| Depth from tiers, focus from accent | Radius, decorative shadows, gradients |
| Square marks and text badges | Circle status dots |
| Fix Sephiro upstream | Fork a Sephiro component in `web/` |
| Motion behind the reduced-motion check | Motion that blocks input |

## Verification

`bun` checks in `web/`, then Playwright screenshots at 390px and 1280px, in steel and one other theme,
light and dark. If this doc and the screenshots disagree, update the doc.
