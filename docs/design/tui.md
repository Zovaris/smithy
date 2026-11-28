---
version: alpha
name: smithy-tui
description: Slab. Terminal workspace for coding agents built from flat bands of colour, square corners, one accent per theme.
# Source of truth: internal/tui/kit/theme.go. Values: steel (default) light / dark, ansi256, ansi16.
colors:
  surface: {light: "#F4F6F9", dark: "#0E1116", ansi256: 233, ansi16: 0}
  raised:  {light: "#EBEFF4", dark: "#131820", ansi256: 234, ansi16: 0}
  focus:   {light: "#E1E7EE", dark: "#1A212B", ansi256: 235, ansi16: 0}
  overlay: {light: "#D8E0E9", dark: "#222B37", ansi256: 236, ansi16: 0}
  border:  {light: "#C3CDD9", dark: "#2C3746", ansi256: 237, ansi16: 8}
  strong:  {light: "#0E1116", dark: "#E6EDF5", ansi256: 255, ansi16: 15}
  text:    {light: "#2C3746", dark: "#B4C0CE", ansi256: 251, ansi16: 7}
  muted:   {light: "#5E6B7D", dark: "#8593A6", ansi256: 103, ansi16: 8}
  dim:     {light: "#8592A3", dark: "#7A879A", ansi256: 103, ansi16: 8}
  accent:  {light: "#C2541A", dark: "#F2762E", ansi256: 208, ansi16: 11}
  ok:      {light: "#1F8A5B", dark: "#4CC38A", ansi256: 78,  ansi16: 10}
  warn:    {light: "#9A6B00", dark: "#E3B341", ansi256: 178, ansi16: 3}
  err:     {light: "#C2362F", dark: "#F47067", ansi256: 203, ansi16: 9}
  info:    {light: "#1F6FCF", dark: "#6CB6FF", ansi256: 75,  ansi16: 12}
  shade:   "surface darkened 45% (Theme.Shade)"
  ramp:    "six dark steps, darkest first (Theme.Ramp)"
glyphs:
  plain: {mark: "▪", mark-off: "▫", rail: "▌", prompt: "›", ok: "✓", err: "✗", brand: "◆", spinner: "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"}
  ascii: {mark: "#", mark-off: ".", rail: "|", prompt: ">", ok: "+", err: "x", brand: "*", spinner: "|/-\\"}
  nerd: optional, never required
spacing: {gutter: 2, indent: 3, pad-x: 1, turn-gap: 1, chip-gap: 1, sidebar: 38}   # cells
rounded: 0
motion: {spinner: 80ms, spring: {model: harmonica, damping: "near critical"}, modal: ~180ms, value: ~180ms}
---

# smithy TUI design: Slab

If this doc and a golden file disagree, the golden wins and this doc is updated.

## Overview

The TUI is where a user drives agents, answers their questions and watches checks. Slab builds every
surface from flat bands of background colour: no borders, no radius. Principles:

- Hierarchy from tiers: Surface < Raised < Focus < Overlay.
- One accent per theme. It marks focus, you and active, nothing else.
- Signals (Ok, Warn, Err, Info) only for state. Evidence over decoration.
- Few concepts: one chip, one rail, one status vocabulary everywhere.

What makes it smithy's, not another agent TUI: the transcript hangs off one forge timeline, a
rail with a one-cell mark gutter (`✓ ✗ $ ?` and the spinner sit on the first line of their block);
your prompt is a bold heading on a heavy accent rail with the turn's result and time on its right,
not a boxed message; tool runs fold into small tallies; the sidebar is a live session panel (pulse
sparkline of steps per turn, heat, work, queue, links) that hides when there is nothing to show;
an empty session opens on the anvil with recent sessions and keys, left-aligned, never a centered
logo and input.

## Colors

Seven themes via `ui.theme`: steel (default), frost, plum, moss, dune, dusk, rose. Each defines the same
tokens in light and dark (`lipgloss.LightDark`). Accents: steel #C2541A / #F2762E, frost #0E7C86 /
#3DD6D0, plum #9333EA / #C084FC, moss #4D7C0F / #9BD35A, dune #A86A00 / #F0B429, dusk #4153D6 / #7C8CFF,
rose #C2255C / #FF6B9A.

| Token | Job |
|---|---|
| Surface | Page |
| Raised | Header, tabs, sidebar, user band, shell band, modal body |
| Focus | Input band, selected row, search field |
| Overlay | Key chips, meter rest, tiles, description panel |
| Border | Rules under headings; all foregrounds under a modal backdrop |
| Accent | Rail, active tab fill, prompt, headings, edited `•`, meter fill |
| Strong / Text / Muted / Dim | Values and user text / agent body / descriptions / timestamps, model |
| Ok / Warn / Err / Info | Done / waits on you or in progress / failed / agent, tool, link |

Text on an Accent or Warn fill is Surface. Body text on every tier stays ≥ 4.5:1 in truecolor.

Fallbacks (colorprofile downsamples):
- 256 colours: the `ansi256` column; the theme keeps its own accent (`a256`).
- 16 colours: the `ansi16` column; the theme keeps its accent (`a16`), other tokens are steel's. Tiers
  collapse, so focus also shows by the rail glyph and bold.
- `NO_COLOR`, `--no-color`, `TERM=dumb`: no colour at all. Every state is still readable from glyph and
  word; the active tab and focused row keep reverse video and the rail.

## Typography

Bold for user text, titles, active tab, agent name, headings. Italic only for "earlier runs, agent not
recorded". Faint is never used for meaning. Headings are sentence case, never uppercase or tracked.
Plain Unicode glyphs by default (see front matter); a Nerd Font is optional and never required. Without
a UTF-8 locale the ASCII set applies.

## Layout

Grid: page gutter `pad` (3 cols from 100 wide, else 1); mark column at `pad`, rail at `pad+2`, text
column at `pad+4`; under 80 columns the mark replaces the rail. Bands (shell output, open tools, the
input) run from one cell before the text column to `width - pad`. The sidebar ends on that same right
edge, after a fixed 2-col gap; header and footer right chips end there too. `TestBlocksShareOneGrid`
checks it at 120 and 200 columns. A short session starts at the top under a 2-line intro (title;
agent, model, branch, started) and grows down; once it fills the screen it stays anchored to the input.
Rhythm: exactly one blank row (the rail runs through it) between blocks; tool rows tight; no
horizontal rules: hierarchy comes from weight, colour and spacing.
An agent answer is plain text on the rail; its agent, model, time and rate sit on the prompt heading
once the turn ends. Text streamed in parts becomes that one answer, never repeated.
Sidebar (34 cols, 40 from 160 wide, Raised) shows from 120 cols when the session has something to show.

80x24:
```
 ◆ smithy  v0.26 · skills                               tend ▪  ⠹ working  14:02
  main  ⠹ fix-ci  ? review  +
                                                                              
▌ fix the flaky test in tests/run.sh                                   14:01  
                                                                              
  builder  sonnet · high                                                      
  I'll read the runner first.                                                 
   ▪ Read  tests/run.sh                                           ✓ 53 lines  
   ▪ Grep  "sleep" tests/                                          ✓ 4 hits  
                                                                              
  $  bash tests/run.sh                                         exit 0 · 1.8s  
     ok  all 41 checks                                                        
                                                                              
▌ Questions                                                                   
▌ Keep the retry or remove it?                                                
▌ › 1 Keep the retry                                                          
▌     Leaves the timing as is                                                 
▌   2 Remove it                                                               
▌     Fix the race instead                                                    
▌   3 Type your own answer                                                    
▌ ↑↓ select · enter submit · esc dismiss                                      
▌ ›                                                                           
  builder  sonnet  effort high  2 queued                            12.4k tok 
 enter send  ctrl+c stop  tab agent                       normal  ctrl+k palette
```

120x40 (sidebar on the right; transcript as above):
```
 ◆ smithy  v0.26 · skills                                                    tend ▪   ⠹ working   14:02              
  main  ⠹ fix-ci  ? review  +                                                  fix the flaky test                    
                                                                                                                     
▌ fix the flaky test in tests/run.sh                                  14:01    Context                               
                                                                               ████████████▒▒▒▒▒▒▒▒▒▒▒▒▒▒  46%     
  builder  sonnet · high                                                                                             
  I'll read the runner first.                                                  Spend                                 
   ▪ Read  tests/run.sh                                          ✓ 53 lines    today                       $0.84     
   ▪ Grep  "sleep" tests/                                          ✓ 4 hits    session                     $0.31     
   ▪ Edit  tests/run.sh                                               ✗ diff                                         
                                                                               Links                                 
  $  bash tests/run.sh                                        exit 0 · 1.8s    ▪ #276 fix flaky runner               
     ok  all 41 checks                                                           ci ⠹ 4/6 · 1 review                 
                                                                                                                     
   ...                                                                         Queue                                 
                                                                               1 rerun on linux                      
▌ Questions ...  (as at 80x24)                                                 2 update CHANGELOG                    
▌ ›                                                                                                                  
  builder  sonnet  effort high  2 queued                       12.4k tok                                             
 enter send  ctrl+c stop  tab agent  / commands                                     normal  ctrl+k palette           
```
(`█` = Accent cells, `▒` = Overlay cells; in the app both are flat background fills.)

Resize: below 120 cols the sidebar hides and its data stays reachable from the palette; chips and
right-aligned results truncate their label first, never the status glyph.

## Elevation

Layers, bottom to top: page, sidebar, ask panel, modal. Modals are lipgloss v2 compositor layers: the
panel, a half-block offset shadow one cell right and below in `Theme.Shade` (Surface darkened 45%),
and a dimmed backdrop where every foreground under the modal collapses to Border.

## Borders

None. A 1-col `▌` Accent rail marks the user band and the input; a Warn rail marks the ask panel; a
Border rule underlines a modal group heading. No box-drawing characters anywhere.

## Motion

| Where | Timing | What moves |
|---|---|---|
| Spinner | 80ms per frame, `Theme.Spinner(frame)` | braille, ASCII `\|/-\` |
| Modal open / close | ~180ms harmonica spring, near critical damping | panel rises and reveals |
| `‹ value ›`, meters | ~180ms spring | value eases to target |

Motion never delays input. `ui.reduce_motion`, `--no-anim` or `SMITHY_REDUCE_MOTION`: no springs,
instant changes, a still spinner frame plus the word "working".

## Voice

English, terse, lower-case labels: `done:`, `exit 0 · 1.8s`, `ci ✓ · 1 review`, `→ Asked 1 question`.
Errors name what failed and what to do next.

## Components

Status vocabulary, the only one: `▪` state mark (Ok, Warn, Err, Dim), `▫` off, `ci ✓` Ok, `ci ✗` Err,
`ci ⠹ 4/6` Warn, Warn `?` chip for waiting on you, spinner for running. Circles (`●◐○`) are banned.

| Component | Anatomy | States |
|---|---|---|
| Header | Raised row; Accent ` ◆ smithy ` block, version and project Muted; right: chips on Overlay for tend, state (`idle`, spinner `N running`, Warn `? waiting on you`) and the clock | default; running (spinner); waiting (Warn chip) |
| Tab chip | ` title ` on Raised; title is the session title without polite openings, cut at a word near 24 cells or middle-ellipsed; `«` `»` when tabs overflow, the active one kept in view | default Muted; active Accent bg, Surface fg, bold; running spinner |
| User turn | Raised band, `▌` Accent, Strong bold, time Dim right | default |
| Agent turn | name Info bold, model and effort Dim, body Text | default; running spinner after name |
| Tool row | `▪ Tool  args` Info, result right | running spinner; done `✓` Ok; failed `✗` Err; disabled Dim |
| Shell band | Raised, Warn `$` chip, command Strong bold, output Muted, `exit n · t` right | running spinner; ok Ok; failed Err |
| Ask panel | "Questions", Warn rail, question bold, numbered options with Muted line, "Type your own answer", hints | default; focus row Focus bg + `›`; answered collapses to transcript line |
| Input | Focus band, Accent rail, Accent `›`; chips: agent, model, effort (Accent), queued (Warn); tokens right | focus; disabled Dim prompt while a modal is open |
| Footer | keys in Text, labels Dim, three spaces apart, no backgrounds; right: mode chip, Accent `ctrl+k palette` chip | default; select mode (Warn) |
| Sidebar | 38 cols Raised; title Strong bold; headings Accent bold; meter Accent / Overlay; spend label Muted, value Strong; links `▪ PR #n title` + `ci ✓ · state`; a links error is one Muted line, e.g. `links paused: GitHub rate limit, retry 14:05`, never the raw error | default only |
| Modal | Raised body, full-width Accent title bar (title Surface bold, `esc` right), shade, dim backdrop | open, closing (spring) |
| /config | `/ filter` on Focus; a curated list of about 20 settings with human labels (Theme, Transcript, Sidebar, Effort, Tend, Serve…) in Look, Agent, Tend, Serve, Notify, then `Advanced (N)` collapsed; filtering searches all of them; booleans read on/off; the config key shows only in the description footer | focus row: Focus bg, rail, every option as a chip, `‹ value ›`; changed Accent `•`; theme row as accent swatches |
| /usage | period chips; tiles spent, runs, tokens out, cache read on Overlay with coloured top tick; 30-day Accent bars (today Strong); by agent and by model with cost bars; no-agent group last, Muted italic; token bar cache Warn, in Info, out Ok | active period Accent chip |
| Doctor | same modal frame, one row per check in the status vocabulary | ok, warn, failed |

Keys: arrows move, enter acts, esc closes the top layer, ctrl+k opens the palette. Mouse clicks select
tabs, options and rows.

## Accessibility

- Never colour alone: every mark has a glyph and a word or count.
- `NO_COLOR` and 16 colours stay usable (see Colors). ANSI 16 is user-remappable, so tiers are never
  the only focus cue.
- Light and dark for every theme; text ≥ 4.5:1 on its tier.
- Reduced motion as above. Keyboard reaches everything.

## Do / Don't

| Do | Don't |
|---|---|
| Take colour only from `kit.Theme`; add a token first | Hex literals in components |
| Show depth with a tier step, focus with Accent | Borders, boxes, rounded corners |
| `▪` / `▫` and text badges for state | `●◐○` or any circle |
| One Accent fill per row (active tab, title bar) | Accent for state, signals for decoration |
| Sentence-case headings | Uppercase or letter-spaced labels |
| Plain glyphs with ASCII fallback | Requiring a Nerd Font |
| Instant input, motion after | Animation that blocks a key, or any with reduce motion on |

## Verification

teatest goldens in `internal/tui/chat/testdata/*.golden` (and other `internal/tui/**/testdata`), screenshots in
`docs/demo/`. Run the TUI for real in steel, one other theme, light and dark, 16 colours and
`NO_COLOR`, at 80x24 and 120x40, before calling a change done.
