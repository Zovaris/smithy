# Settings and setup

Settings live in `<repo>/.smithy/config.json` (this repo) and `~/.smithy/config.json` (the
user's defaults), repo first. `smithy config list` shows each value, its source and what uses
it; `smithy config set [--repo] <key> <value>` changes one. Nobody edits JSON by hand.

- **Up front** ("use-smithy setup"):
  1. Run `smithy setup status`.
  2. Ask every unset setting in one numbered message, each with your recommendation ("yes"
     accepts all); save the answers.
  3. Show the block below and offer `smithy setup block add <file>`; add it only on a yes.
  4. If `setup status` says smithy is not on PATH, offer once `smithy setup path add` (one line
     in the user's shell startup file) so `smithy status` works in a terminal.
- **On first use**: a skill that needs an unset setting asks just that one (model tiers: every
  unset tier in one message), once, and saves it: `--repo` for answers about this repo (where
  specs go), user-level otherwise (models, merge and cleanup defaults).

## Agent instructions block

`smithy setup block add` copies this block, markers included:

<!-- use-smithy:start -->
## Agent skills

This machine has the smithy skills installed (use-smithy, smelt, warden, ledger, scout, tend,
hush, simple). Before any software work, load the `use-smithy` skill and follow it: it says
which installed skill fits (build with smelt, parallel tasks with warden, decisions and docs
with ledger, facts with scout, open PRs with tend) and the rules they share.
<!-- use-smithy:end -->
