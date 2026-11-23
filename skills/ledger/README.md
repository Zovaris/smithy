# ledger

Project docs that live next to the code and are reviewed in the same PR: decision records
(ADRs), a glossary, module READMEs, a codebase map for agents, drift checks, and the comment
policy (API doc comments only; the "why" lives in docs).

Started by the agent when a decision or doc is involved, or by you: `/ledger …`.

## Modes

| Mode | Does |
|---|---|
| `init` | Sets up `docs/`: ADR 0000, glossary, standards, a pointer in your agent instructions |
| `adr <title>` | The next numbered ADR, filled from the conversation and the code |
| `standard <rule>` | Adds, merges or retires a rule in `docs/agents/STANDARDS.md` |
| `module <path>` | A README for that module: purpose, entry points, invariants |
| `map` | `docs/agents/MAP.md`, so agents stop re-exploring the repo |
| `sync [base]` | Finds docs the diff made stale (`smithy drift`) and updates them |
| `comments [paths]` | Flags explanatory comments, TODOs and commented-out code (`smithy comments`) |
| `search <query>` | Searches past decisions before anything is re-decided |

A decision gets an ADR only when it is hard to reverse, surprising, and a real trade-off.
