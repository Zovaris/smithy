# use-smithy

The router. Your agent loads it on its own at the start of any coding work; it lists the
installed smithy skills, sends each piece of work to the one that fits and holds the rules they
all share.

**Install it with everything else.** Without it, the agent only knows the other skills by their
one-line descriptions, and the user-invoked ones never come up unless you type them.

## What it does

| You want… | It routes to |
|---|---|
| to build, change or fix code | **smelt** (`fast` for small changes) |
| several tasks done at once | **warden**, after you say yes (it spends sessions) |
| a fact, a comparison, "why does X" | **scout** |
| a decision recorded, docs, a codebase map | **ledger** |
| open PRs kept green, new PRs or issues watched | **tend** |
| the work done without conversation | **hush** |

Shared rules: one task list, evidence before "done", atomic commits in the repo's style, waiting
on CI with `smithy wait` instead of sleep loops, models picked per task (cheap, mid, strong),
and only the user's decisions go to the user.

## Setup

Ask your agent to **"run use-smithy setup"**. In one message it:

1. shows what is installed and every setting (`smithy setup status`);
2. asks the unset ones, each with a recommendation;
3. offers a short pointer block for your `AGENTS.md` / `CLAUDE.md`;
4. offers to put `smithy` on your `PATH`, so you can run `smithy status` yourself.

Nothing is written without a yes.
