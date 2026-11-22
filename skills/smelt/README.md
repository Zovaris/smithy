# smelt

One engineering task, step by step: brainstorm → spec → task list → test-first build →
independent test agents → review → PR → retro. Start at any phase, at the speed the task needs.
Started by you (by name, the way your harness starts skills) or by an agent through use-smithy;
`smelt <task>` picks the entry, and SKILL.md lists every command.

| Speed | For | Agents (one wave, in parallel) | Target |
|---|---|---|---|
| fast | small or clear changes | none (you check against the spec) | ~5 min |
| normal (default) | most features and fixes | adversary + one reviewer | ~10–15 min |
| full | money, auth, data loss, public APIs | adversary, property, reviewer, security, mutation reader; hostile last | the safest flow |

Every speed builds test-first and passes the verify gate: no "done" without fresh evidence.

You get a spec and a task list (`NN-<slug>.md`), atomic commits in your repo's style, a PR filled
from your template, and a branch named by `smithy names branch`, cleaned up after merge. Hand
the task list to **warden** to build it in parallel.
