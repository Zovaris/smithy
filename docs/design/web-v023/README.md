# Web design pass (v0.23): Hearth

Design for approval before the web screens are built (spec §5). Open `mock.html` in a browser
(`?mode=dark`, `?view=sheet`, `?view=new`); `phones.html` shows the three phone states side by side.

**Theme:** `web/src/theme/hearth.css`, a Sephiro theme: warm paper and ink, one ember accent shared
with the TUI's Steel, light/dark from the system with a `data-mode` override for the toggle.
Status always pairs a color with a glyph (● working, ? needs you, ✓ done, ✕ failed).

**Layout (mobile-first):**
- Phone (<1024px): one column, top bar with session title, status and `+/−` changes chip; sessions
  open as a bottom sheet; composer pinned with safe-area insets.
- Laptop (≥1024px): sessions sidebar grouped per server (§6b) + centered 720px conversation.
- Wide (≥1280px): adds the right rail: Changes (file list, diff, inline comment sent with the next
  message), Decisions, Agents.
- Decisions (guard, permission, plan approval) render inline in the conversation and in the rail.
- New session: serif greeting, detected agents as ready/missing chips (§3), skill-shaped starters.

**Grounded in** the v0.23 research (`research.md` §3): Claude Code on the web (sidebar, Changes card
with inline comments), Jules (plan approve), OpenCode web (same server, QR pairing), Conductor and
Sculptor (all agents visible at a glance). Traits taken: sessions left, conversation center,
changes right, one composer with `/`, needs-you status loud.

| | Light | Dark |
|---|---|---|
| Desktop 1440 | ![](desktop-light.png) | ![](desktop-dark.png) |
| New session | ![](new-light.png) | ![](new-dark.png) |
| Phone 390 | ![](phone-light.png) | ![](phone-dark.png) |
