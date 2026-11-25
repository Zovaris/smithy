# Install and update

## Skills

```bash
npx skills add Zovaris/smithy -g                                          # pick skills and agents
npx skills add Zovaris/smithy -g -y --skill '*' -a claude-code -a codex   # no prompts
npx skills add Zovaris/smithy@v0.19.0 -g                                  # pin a release
```

Drop `-g` to install into the current project only. This needs the
[skills CLI](https://github.com/vercel-labs/skills) (Node 22.20+) and git.

## Binary

```bash
curl -fsSL https://raw.githubusercontent.com/Zovaris/smithy/main/install.sh | sh              # latest
curl -fsSL https://raw.githubusercontent.com/Zovaris/smithy/main/install.sh | sh -s 0.19.0    # pinned
curl -fsSL https://raw.githubusercontent.com/Zovaris/smithy/main/install.sh | sh -s -- --update  # skills + binary
```

[`install.sh`](../install.sh) downloads the binary for your platform from the GitHub release into
`~/.smithy/bin/<version>/`. It verifies two things before installing:

- **Checksum:** a mismatch always refuses.
- **GitHub build attestation:** this needs `gh`. Without `gh`, nothing is installed unless you set
  `SMITHY_ALLOW_UNATTESTED=1`, which accepts the checksum alone.

Each skill ships a small launcher that runs the binary and never downloads anything. If the
installed version doesn't match the skills, the agent asks whether to run `install.sh` for you.

To use `smithy` in your own terminal, accept the PATH offer in use-smithy setup. On Windows, add
`%USERPROFILE%\.smithy\bin` to your user PATH.

## Harness build

The release also ships `smithy-harness-<os>-<arch>`: the same commands plus `smithy daemon`,
`smithy attach`, `smithy board` and `smithy chat` (bare `smithy` opens the chat) (ADR 0009). On a terminal `install.sh` asks "harness or
skills" (Enter picks skills); `--harness` or `--skills` skips the question, and without a terminal
it installs the skills build and prints how to install the skill files. `--update` keeps the kind you installed (saved in `~/.smithy/kind`) and updates the skills with `npx skills update`, or with `smithy skills install` when Node is missing. Or build
it with `go build -tags harness ./cmd/smithy`.

```bash
curl -fsSL https://raw.githubusercontent.com/Zovaris/smithy/main/install.sh | sh -s -- --harness
```

## GitHub access

| You have | smithy uses |
|---|---|
| `gh` signed in | the GitHub CLI (recommended) |
| `GH_TOKEN` or `GITHUB_TOKEN` | the GitHub API |
| neither | pushes the branch and prints a compare URL |

## Keys

API keys live in `~/.smithy/auth.json` (mode 0600), never in a config file. Set them with
`smithy auth set <name>`, for example `smithy auth set linear < keyfile`.

Any process running as you can read that file, agents included, so use test or least-privilege
keys.
