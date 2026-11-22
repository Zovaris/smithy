# smithy

**Agent skills for planned, test-first, parallel software work.**
smithy makes your coding agent plan before it builds, write the failing test first, have
independent reviewers check the result, and keep pull requests green until you merge them.

[![check](https://github.com/Zovaris/smithy/actions/workflows/check.yml/badge.svg)](https://github.com/Zovaris/smithy/actions/workflows/check.yml)
[![release](https://img.shields.io/github/v/release/Zovaris/smithy?sort=semver&label=release)](https://github.com/Zovaris/smithy/releases)
[![license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

## Install

```bash
npx skills add Zovaris/smithy -g
```

Pick every skill (at least `use-smithy`) and your agents, then tell your agent: "run use-smithy setup".
Needs the [skills CLI](https://github.com/vercel-labs/skills) (Node 22.20+) and git. The
[GitHub CLI](https://cli.github.com) (`gh`) is recommended, not required: without it smithy uses
`GH_TOKEN`/`GITHUB_TOKEN`, or pushes the branch and prints a compare URL. GitHub only for now.

## Skills

| Skill | Purpose |
|---|---|
| [use-smithy](skills/use-smithy/README.md) | Routes work to the right skill and holds the shared rules. Install it. |
| [smelt](skills/smelt/README.md) | One task: spec, test-first build, review agents, receipts, secret scan, PR. |
| [warden](skills/warden/README.md) | Many tasks at once: one silent worker per task in its own worktree, each to a PR. |
| [tend](skills/tend/README.md) | Keeps PRs mergeable: CI fixes, rebases, review comments. |
| [ledger](skills/ledger/README.md) | Project docs in the repo: ADRs, glossary, standards, drift checks. |
| [scout](skills/scout/README.md) | Read-only answers with cited sources. |
| [hush](skills/hush/README.md) | Does the task with no talk; ends with one `done:` line. |
| [simple](skills/simple/README.md) | Short, plain replies with no filler. |

## Quick start

Say these to your agent in plain words:

1. "Add a `--json` flag to the export command." (planned, built test-first, ends in a PR)
2. "Do these five tasks in parallel." (warden asks for your yes, then one worker and PR each)
3. "Keep my open PRs green." (tend fixes CI, rebases, answers review comments)

You decide the plan, scope and merges: nothing reaches your main branch without your yes.

## Speeds

smelt runs at `fast`, `normal` (default) or `full`. Figures are single-task measurements from the
[trials](docs/trials/); agents are the cost, about 60k tokens each.

| Speed | What runs | Typical time | Typical tokens |
|---|---|---|---|
| `fast` | Spec from the task, test-first on the criteria, scripts, you check the diff | ~3 min | ~50k |
| `normal` | Spec, test-first build, adversary and one reviewer (security if risky) | ~5-15 min | ~220k (up to ~500k on risky code) |
| `full` | Brainstorm and spec, every test agent, one reviewer, security, mutation | ~25 min | ~500k-760k |

For comparison, one plain session on a small project took ~5 min and ~150k tokens (trial 7):
smithy costs more and catches more bugs before merge.

## Harnesses

Works in any harness that loads `SKILL.md` skills: Claude Code, Codex, OpenCode, Cursor, Gemini
CLI, Pi, DeepSeek Harness. Single-task skills behave the same everywhere; warden depends on what
the harness offers (details in [harness.md](skills/warden/references/harness.md)):

| Harness | How warden runs workers |
|---|---|
| Claude Code (desktop, CLI) | Separate sessions, one per task; it can be woken when one ends. |
| OpenCode, DeepSeek Harness | Parallel headless sessions via `smithy agent`; woken when one ends. |
| Gemini CLI | Parallel headless sessions; woken only with `backgroundCompletionBehavior: inject`. |
| Codex, Pi, Cursor CLI | Parallel headless sessions; warden waits in the foreground. |
| Any other, or no way to start sessions | Workers as background subagents of the session; or build the list one task at a time (`/smelt build next`). Pi has no subagents. |

Headless workers are one-shot: they can't take messages mid-run, so a question is answered by
respawning the worker.

## Docs

- [docs/settings.md](docs/settings.md): every setting and its default
- [docs/adr](docs/adr): why things are built this way
- [docs/trials](docs/trials): measured runs behind the speeds and agent counts
- [CONTRIBUTING.md](CONTRIBUTING.md) and [CHANGELOG.md](CHANGELOG.md)

[MIT](LICENSE)
