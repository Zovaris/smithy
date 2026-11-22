# Settings and setup

All settings live in two JSON files, repo first: `<repo>/.smithy/config.json` (this repo only)
and `~/.smithy/config.json` (the user's defaults). `smithy config list` shows every setting
with its value, where it came from and what uses it; `smithy config set [--repo] <key> <value>`
changes one. Nobody has to edit JSON by hand.

Two ways to fill them:
- **Up front** — when the user says "use-smithy setup" (or `/use-smithy setup`): run
  `smithy setup status`, then ask every unset setting in one message (numbered, each with your
  recommendation, "yes" accepts all), save the answers, and offer the agent-instructions block
  (`smithy setup block add <file>`, only with a yes, after showing `references/AGENTS-BLOCK.md`).
  Then, if `setup status` says smithy is not on PATH, offer once to make `smithy` a terminal
  command for the user (`smithy status`, `smithy tasks board --all`): on a yes,
  `smithy setup path add` (one line in their shell's startup file).
- **On first use** — any skill that needs an unset setting asks just that one (model tiers: every unset one in one message), once, and saves
  it: `--repo` when the answer is about this repo (where specs go), user-level otherwise
  (models, merge and cleanup defaults).
