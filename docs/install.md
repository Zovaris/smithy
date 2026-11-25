# Install and update

## Skills

```bash
npx skills add Zovaris/smithy -g                                          # pick skills and agents
npx skills add Zovaris/smithy -g -y --skill '*' -a claude-code -a codex   # no prompts
npx skills add Zovaris/smithy@v0.22.0 -g                                  # pin a release
```

Drop `-g` to install into the current project only. This needs the
[skills CLI](https://github.com/vercel-labs/skills) (Node 22.20+) and git.

## Binary

```bash
curl -fsSL https://raw.githubusercontent.com/Zovaris/smithy/main/install.sh | sh              # latest
curl -fsSL https://raw.githubusercontent.com/Zovaris/smithy/main/install.sh | sh -s 0.22.0    # pinned
curl -fsSL https://raw.githubusercontent.com/Zovaris/smithy/main/install.sh | sh -s -- --update  # skills + binary
```

[`install.sh`](../install.sh) downloads the binary for your platform from the GitHub release into
`~/.local/share/smithy/versions/<version>/` (`$XDG_DATA_HOME`) and links `~/.local/bin/smithy`
(`$XDG_BIN_HOME`) to it; on Windows `%LOCALAPPDATA%\smithy\versions\<version>\` and
`%LOCALAPPDATA%\Programs\smithy\smithy.exe`. Keys, the db, config and plans stay in `~/.smithy`.
An install from before v0.21 in `~/.smithy/bin` moves there; `~/.smithy/bin` keeps links to it for
one release, so older skills and PATH entries keep working. It verifies two things before installing:

- **Checksum:** a mismatch always refuses.
- **GitHub build attestation:** this needs `gh`. Without `gh`, nothing is installed unless you set
  `SMITHY_ALLOW_UNATTESTED=1`, which accepts the checksum alone.

Each skill ships a small launcher that runs the binary and never downloads anything. If the
installed version doesn't match the skills, the agent asks whether to run `install.sh` for you.

If `~/.local/bin` is not on your PATH, `install.sh` prints the exact line for your shell (zsh, bash
or fish) and, on a terminal, offers to add it (default no); use-smithy setup offers the same. On
Windows, add `%LOCALAPPDATA%\Programs\smithy` to your user PATH. `smithy doctor` shows the binary
version and whether `~/.local/bin` is on PATH.

## Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/Zovaris/smithy/main/uninstall.sh | sh   # or: smithy uninstall
```

It lists everything it would remove, with sizes, then asks (without a terminal it needs `--yes`):
the installed versions and the `smithy` command (plus the old `~/.smithy/bin`), the skill folders
`smithy skills install` wrote (only those), a running daemon, `smithy-*` container images left by older versions, the
`smithy-internal`/`smithy-egress` networks, and the PATH line install.sh or setup added.
`~/.smithy` stays unless you pass `--purge`, and then only after you type `purge` (without a
terminal: `--purge --confirm-purge`). Skills installed with `npx skills` are removed with
`npx skills remove`.

## Harness build

The release also ships `smithy-harness-<os>-<arch>`: the same commands plus `smithy daemon`,
`smithy attach`, `smithy board`, `smithy chat` and `smithy web` (bare `smithy` opens the chat) (ADR 0009). On a terminal `install.sh` asks "harness or
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

API keys live in `~/.smithy/auth.json` (mode 0600, AES-GCM encrypted with `~/.smithy/auth.key`, kept next to it), never in a config file. A plaintext `auth.json` from an older smithy is encrypted in place on first use, keys kept. Back up `auth.key` with `auth.json`: without it the keys cannot be read (delete both and set the keys again). Set them with
`smithy auth set <name>`, for example `smithy auth set linear < keyfile`.

Under `--profile auto` the allow-lists for claude and opencode deny reading both files, but any
process running as you can read `auth.json` and `auth.key`, agents included, unless the optional
OS sandbox (`auto.sandbox`, off by default) is on. Use test or least-privilege keys.
