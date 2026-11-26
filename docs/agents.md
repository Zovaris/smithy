# For AI agents: install and set up smithy

Follow these steps in order. Each step has a check. Stop and tell the user if a check fails.

## 1. Install the skills

Pick the flag for the harness you run in, then run:

```bash
npx skills add Zovaris/smithy -g -y --skill '*' -a <harness>
```

`<harness>` is the skills CLI id of your agent:

| Harness | `-a` id |
|---|---|
| Claude Code | `claude-code` |
| Codex | `codex` |
| OpenCode | `opencode` |
| Cursor | `cursor` |
| Antigravity CLI | `antigravity-cli` |
| GitHub Copilot CLI | `github-copilot` |
| Grok Build | `grok` |
| Pi | `pi` |

DeepSeek Harness has no id in the skills CLI: skip this step and let step 2 install the skills.

Check: the `use-smithy` skill is in your skills folder (see [harnesses.md](harnesses.md) for the
folder per harness).

If Node is not available, skip this step: step 2 installs the skills too.

## 2. Install the binary (ask first)

The skills need the `smithy` binary. **Ask the user before running `install.sh`.** It downloads and
installs a binary and copies the skills into every agent it finds. Run it only on a clear yes:

```bash
curl -fsSL https://raw.githubusercontent.com/Zovaris/smithy/main/install.sh | sh
```

Add `-s -- --no-skills` after `sh` if step 1 already installed the skills.

If it stops with "can't verify the build attestation", tell the user. They need `gh` signed in,
or they can choose to set `SMITHY_ALLOW_UNATTESTED=1`. Never set it yourself.

## 3. Verify

```bash
smithy --version
smithy doctor
```

Check: `--version` prints a version, and `doctor` reports no errors. If `smithy` is not found,
`~/.local/bin` is not on PATH: run `~/.local/bin/smithy` directly and show the user the PATH line
install.sh printed.

## 4. Run setup

Read the installed `use-smithy` skill and run its setup with the user ("run use-smithy setup").
It asks which models to use and saves them. Let the user answer each question.

## Keys

Store API keys only with:

```bash
smithy auth set <name>          # in a terminal it prompts, echo off
smithy auth set linear < keyfile
```

Keys go to `~/.smithy/auth.json`, encrypted. Never write a key into a config file, a repo file or
a command line that ends up in shell history.

## Update and uninstall

```bash
smithy update            # binary and skills to the latest release
smithy update --check    # compare versions, change nothing
smithy uninstall         # lists what it removes, then asks
```

`smithy uninstall` needs `--yes` without a terminal. Ask the user before passing it. Skills
installed with `npx skills` are removed with `npx skills remove`.

## Do not

- Run `install.sh`, `smithy update` or `smithy uninstall` without the user's yes.
- Set `SMITHY_ALLOW_UNATTESTED=1` or pass `--force`, `--purge` or `--confirm-purge` on your own.
- Store keys anywhere but `smithy auth set`.
- Edit files in `~/.smithy` or `~/.local/share/smithy` by hand.
- Answer setup questions for the user.

## Troubleshooting

| Symptom | Fix |
|---|---|
| `smithy: command not found` | Use `~/.local/bin/smithy`; add `~/.local/bin` to PATH (install.sh prints the line). |
| Attestation check fails | Install `gh` and run `gh auth login`, then rerun install.sh. |
| Skills say the version does not match | Ask the user, then run `smithy update`. |
| `smithy update` refuses a dev build | The binary is not `~/.local/bin/smithy`. Tell the user; `--force` is their call. |
| No PR is opened | Without `gh` or `GH_TOKEN`, smithy pushes and prints a compare URL. That is expected. |

More detail: [install.md](install.md).
