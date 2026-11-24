# Roadmap

What is planned and what is waiting. The same items are tracked in Linear (project **Smithy**).

## v0.17: close the gaps in what shipped

| Item | Why |
|---|---|
| Egress allow-list proxy for `auto.sandbox container` | `bridge` is open egress today. A proxy that lets through only GitHub, the model's API and package registries would bring container mode to the level of `native`, for every harness. |
| Usage-limit detection for OpenCode, Gemini CLI, Cursor, Pi and DeepSeek Harness | The pool only rotates for them. Each CLI's real limit message has to be captured from a live run before it can be detected. |
| `native` sandbox for more harnesses | Today only Claude Code is fully covered; Codex runs with network off. Gemini CLI and OpenCode have their own sandbox options to look into. |
| Live tests still pending | Claude Code inside `container`, the pool with two real accounts, and the TUI settings editor. |

## v0.18

| Item | Why |
|---|---|
| GitLab as a forge | GitHub is the only forge today. GitLab needs PRs (MRs), CI waits and issues through `glab` or its API. |

## Backlog

| Item | Notes |
|---|---|
| Bitbucket as a forge | After GitLab. |
| Jira as an issue source for autonomous mode | Out of scope since v0.16; GitHub and Linear only today. |
| Webhooks instead of polling | `smithy auto serve` polls today. |
| Pool together with `container` | The account dir isn't mounted and the pooled login isn't passed, so doctor refuses the combination. |
| Sandbox for the orchestrator | Sandboxes don't nest, so the orchestrator runs on its guard only and repo tests it runs go unsandboxed. |
| SSH remotes and GPG signing inside `native` | Git goes over HTTPS with gh's credential helper; GPG signing doesn't work, and SSH signing is macOS only. |
| smithy allow-list for harnesses other than Claude Code | Under `--profile auto` the others run in their own headless mode with no smithy guard. |
