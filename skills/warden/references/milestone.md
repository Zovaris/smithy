# Milestone review

A milestone is a batch (or release branch) whose items all merged. smelt reviews each task;
this review checks the whole milestone for drift between parts that no single task saw.

## Offer
After step 5's final message, when the batch was a milestone (a `release/*` base, or the user
called it one), add one line: "Run a cheap-model coherence review? How many rounds (1..N)?"
No answer, or under hush: skip it and list it under **Followups**. Never run it unasked.

## Topics
The user names a topic: that one only. Otherwise warden picks one per round, in this order,
skipping topics the milestone didn't touch:
1. contradictions between skills (two rules that can't both hold);
2. duplicated rules (the same rule in two places; keep the owner, point from the other);
3. size: each SKILL.md and reference against the repo's limits, in lines and tokens;
4. docs vs code: commands, flags and paths the docs name that the code lacks, and the reverse;
5. TUI vs CLI gaps: actions one surface offers that the other doesn't.

## Round
1. Launch one read-only worker on the `cheap` tier (`references/models.md`), kind scout, on the
   milestone's merged base, with the round's topic and the diff since `base_sha[<base>]`.
2. Its report lists findings with file:line, answer first; no edits.
3. Real findings go to one fix item (`smithy batch add`, kind ship, branch from `smithy names
   branch`), built and merged like any item; dropped findings get one line each.
4. Next round starts after the fix merges, so it reviews the fixed text.

Done when every round's fix item merged or the user stopped; the final message names each round's
topic and its fix PR.
