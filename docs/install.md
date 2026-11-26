# Install and update

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/Zovaris/smithy/main/install.sh | sh                  # latest
curl -fsSL https://raw.githubusercontent.com/Zovaris/smithy/main/install.sh | sh -s 0.23.1        # pinned
curl -fsSL https://raw.githubusercontent.com/Zovaris/smithy/main/install.sh | sh -s -- --no-skills  # binary only
```

One install gets everything: the `smithy` binary (daemon, TUI, web and the CLI the skills call)
and the skills, which it copies into every agent it finds with `smithy skills install` (no Node
needed). `--no-skills` skips that step. `--harness` and `--skills` from older scripts are accepted
and ignored (ADR 0014). `npx skills add Zovaris/smithy -g` still works if you prefer the skills CLI.

## Update

```bash
smithy update            # binary + skills to the latest release
smithy update 0.23.1     # pin a version
smithy update --check    # print the current and latest version, change nothing
```

`smithy update` downloads `install.sh` from `main` and runs it with `--update`, so the same
checksum and attestation checks apply (`curl ... | sh -s -- --update` does the same). It refuses a
binary that is not `~/.local/bin/smithy` (a dev build) unless you pass `--force`.

[`install.sh`](../install.sh) downloads the binary for your platform from the GitHub release into
`~/.local/share/smithy/versions/<version>/` (`$XDG_DATA_HOME`) and links `~/.local/bin/smithy`
(`$XDG_BIN_HOME`) to it; on Windows `%LOCALAPPDATA%\smithy\versions\<version>\` and
`%LOCALAPPDATA%\Programs\smithy\smithy.exe`. Keys, the db, config and plans stay in `~/.smithy`.
It keeps the new version and the one it replaced, nothing older (a folder there not named like a
version stays). An install from before v0.21 in `~/.smithy/bin` moves there once, its links and
`~/.smithy/kind` go away (anything else in `~/.smithy/bin` stays); later runs say nothing about it. The skills never download: with no binary they print the install line, with
another version they name `smithy update <version>`. It verifies two things before installing:

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

## Building from source

`go build -tags harness ./cmd/smithy` builds what the release ships. `go build ./cmd/smithy`
without tags gives the standard-library-only build, kept for development; its harness-only
commands print how to get the full build (ADR 0014).

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
