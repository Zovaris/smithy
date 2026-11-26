<div align="center">

# smithy

**Agent skills for planned, test-first, parallel software work.**

[![check](https://github.com/Zovaris/smithy/actions/workflows/check.yml/badge.svg)](https://github.com/Zovaris/smithy/actions/workflows/check.yml)
[![release](https://img.shields.io/github/v/release/Zovaris/smithy?sort=semver&label=release)](https://github.com/Zovaris/smithy/releases)
[![Go](https://img.shields.io/github/go-mod/go-version/Zovaris/smithy?logo=go&logoColor=white&label=Go)](go.mod)
[![license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

[![Agent Skills](https://img.shields.io/badge/format-SKILL.md-8b7fe0)](#skills)
[![Claude Code](https://img.shields.io/badge/Claude_Code-supported-d97757?logo=claude&logoColor=white)](#harnesses)
[![Codex](https://img.shields.io/badge/Codex-supported-412991?logo=openai&logoColor=white)](#harnesses)
[![OpenCode](https://img.shields.io/badge/OpenCode-supported-24292f)](#harnesses)
[![Cursor](https://img.shields.io/badge/Cursor-supported-000000)](#harnesses)
[![Gemini CLI](https://img.shields.io/badge/Gemini_CLI-supported-4285f4?logo=googlegemini&logoColor=white)](#harnesses)

[Install](#install) · [How it works](#how-it-works) · [Skills](#skills) ·
[Autonomous mode](#autonomous-mode) · [Harnesses](#harnesses) · [Docs](#docs)

</div>

smithy turns your coding agent into a small engineering team: it plans before it builds, writes
the failing test first, runs tasks in parallel and keeps every pull request green until it merges.

## Highlights

- **Plans first.** Brainstorm, spec and a task list before any code.
- **Test-first.** Every change starts from a failing test.
- **Proof, not claims.** "Tests pass" is a recorded receipt in the PR, and secrets are scanned before every push.
- **Parallel.** Many tasks at once, one worktree and one PR each.
- **PRs stay green.** CI fixes, rebases and review threads are handled for you.
- **You decide.** Nothing reaches production without your yes.
- **Learns with your yes.** Lessons backed by evidence go into your repo's standards.
- **Runs unattended.** Label an issue `auto` and it gets built.
- **Any harness.** Claude Code, Codex, OpenCode, Cursor, Gemini CLI, Pi and DeepSeek Harness.

## Install

```bash
npx skills add Zovaris/smithy -g
curl -fsSL https://raw.githubusercontent.com/Zovaris/smithy/main/install.sh | sh
```

The first line installs the skills; pick at least `use-smithy`. The second installs the `smithy`
binary, after verifying its checksum and build attestation; it asks for the **harness** build
(daemon, TUI and web: run `smithy`) or the **skills** build (only the CLI the skills call).
Skip the question with `sh -s -- --harness` or `sh -s -- --skills`. Without Node, install the
binary first, then run `smithy skills install` (it copies the release's skills into each harness it finds).

Then, in your agent:

1. **"run use-smithy setup"** to choose models and settings.
2. **Describe a task** (or `/smelt <task>`): it gets planned, built test-first and opened as a PR.
3. **Hand over several tasks** (or `/warden <tasks>`): you get one worker and one PR for each.

Requires git. [`gh`](https://cli.github.com) is recommended; without it, smithy falls back to
`GH_TOKEN` or prints a compare URL. The binary lands in `~/.local/bin/smithy`. More options:
[docs/install.md](docs/install.md).

### Update and uninstall

```bash
smithy update      # skills + binary, same build; --check only compares versions
# or: curl -fsSL https://raw.githubusercontent.com/Zovaris/smithy/main/install.sh | sh -s -- --update
smithy uninstall   # lists what it removes, then asks
```

### For agents

If you are an AI agent asked to install smithy, follow these steps:

1. **Install the skills:** run `npx skills add Zovaris/smithy -g -y --skill '*' -a <your-harness>`.
2. **Ask about the binary:** ask the user before running `install.sh`, then run it only on their yes.
3. **Run setup:** read the installed `use-smithy` skill and run its setup with the user.

Never store keys in config files: use `smithy auth set <name>`. Details are in
[docs/install.md](docs/install.md).

## How it works

![How the smithy skills work together](docs/flow.svg)

| Step | What happens |
|---|---|
| **Route** | use-smithy picks the right skill for each request |
| **Plan** | smelt writes a task list (`NN-<slug>.md`) shared with warden |
| **Build** | one task with smelt, or many in parallel with warden |
| **Prove** | `smithy receipt` records test runs; `smithy scan` blocks secrets |
| **Keep green** | tend fixes CI, rebases and answers reviews |
| **Learn** | up to 3 lessons per PR, each with evidence, kept only on your yes |

## Skills

| Skill | What it does |
|---|---|
| [use-smithy](skills/use-smithy/README.md) | Routes work to the other skills. **Install it.** |
| [smelt](skills/smelt/README.md) | One task end to end: spec, TDD, review, PR |
| [warden](skills/warden/README.md) | Many tasks in parallel, one worker and PR each |
| [tend](skills/tend/README.md) | Keeps PRs mergeable and watches the repo |
| [ledger](skills/ledger/README.md) | ADRs, glossary, standards and doc drift |
| [scout](skills/scout/README.md) | Read-only answers with sources |
| [hush](skills/hush/README.md) | Does the task with no talk |
| [simple](skills/simple/README.md) | Short, plain replies |

## Autonomous mode

Label an issue `auto` on GitHub or Linear, and smithy builds it unattended and reports back.

![How autonomous mode works](docs/auto.svg)

```bash
smithy config set auto.sources github,linear
smithy auto serve      # Esc/q stops it
smithy tui             # queue, running agents, cost, settings
```

The spend is bounded by a daily cap and an hours window. Workers can run in a sandbox, and an
opt-in pool spreads the work across your own accounts. Read
[docs/autonomous.md](docs/autonomous.md) before running it: issue text can carry injected
instructions.

## Harnesses

| Harness | Start a skill | Warden workers |
|---|---|---|
| Claude Code (desktop, CLI) | `/smelt` | app sessions, `claude --bg` |
| Codex CLI | `$smelt` | `codex exec` |
| OpenCode | `@smelt` | `opencode run` |
| Cursor CLI | `/smelt` | `agent -p` |
| Gemini CLI | "use smelt" | `gemini -p` |
| Pi | `/skill:smelt` | `pi -p` |
| DeepSeek Harness | `/smelt` | `dsh headless` |

Skills folders, background behavior and custom agents: [docs/harnesses.md](docs/harnesses.md).

## The smithy binary

![How a skill runs the smithy binary](docs/binary.svg)

One small Go binary does the mechanical work: settings, branch names, task lists, commits, PRs and
waiting on CI without spending tokens.

```bash
smithy wait pr:42                    # until checks finish, merge, conflict or review
smithy status                        # every task list and batch on this machine
smithy receipt run -- go test ./...  # record a test run for the PR
smithy scan                          # secrets in what the change adds
```

`smithy help` lists the commands by task; `smithy help <command>` shows usage, flags and
examples for one (`smithy help --all` adds the commands the skills call).

## Docs

- [Install and update](docs/install.md)
- [Settings](docs/settings.md)
- [Autonomous mode](docs/autonomous.md)
- [Harnesses](docs/harnesses.md)
- [API backend and benchmark](docs/api-backend.md)
- [Changelog](CHANGELOG.md) · [Contributing](CONTRIBUTING.md)

GitHub is the only forge today; GitLab and Bitbucket come later.

## Credits

Ideas borrowed from [firstmate](https://github.com/kunchenguid/firstmate),
[dsh-superagent-prototype](https://github.com/deepseek-ai/dsh-superagent-prototype),
[mattpocock/skills](https://github.com/mattpocock/skills),
[superpowers](https://github.com/obra/superpowers) and
[scriptify](https://github.com/pfcoperez/scriptify).

## License

[MIT](LICENSE)
