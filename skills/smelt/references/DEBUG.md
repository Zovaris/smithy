# Debugging

Phases with gates; don't skip ahead. No fix before a root cause.

1. **Reproduce.** Get one fast command that fails on this exact bug, every time: a failing reproduction taken from the report, run and confirmed red before any change, at every speed. No theories
   until it exists. Commit the repro test alone, then on that clean commit run
   `smithy receipt run --claim repro -- <cmd>` (must fail; a dirty-tree run doesn't count), then fix, so `smithy receipt check-red` can prove red-then-green. Can't reproduce → collect what's missing (logs, input, env) and say so.
2. **Shrink.** Make the repro as small and fast as possible (smallest input, single test file).
3. **Locate.** In a multi-component path (UI → API → service → DB, or build → package → run),
   log what goes in and out at every boundary in one run, then find the first boundary where
   the data is wrong. Trace backwards from there to where the bad value was created.
   Single function with no boundaries: state the faulty line and skip tracing.
4. **Hypothesize.** Write 3–5 ranked hypotheses, each with a prediction you can test
   ("if it's the cache, clearing it makes the repro pass").
5. **Probe.** Test the top hypothesis with the cheapest probe, one change at a time. Tag every
   debug log line (`[smelt-debug]`) so cleanup is a grep. Update the ranking after each probe.
6. **Fix.** You write the regression test: turn the repro into one at the right seam; it must fail. Fix the root
   cause, not the symptom. Test green, full suite green, then the repro receipt again on the final
   clean HEAD (`--claim repro`) so `check-red` sees red then green.
7. **Clean.** `grep -rn "\[smelt-debug\]"` returns nothing. Note the root cause in the PR body.
   A cause that will bite again (bad pattern, missing guard) → propose a check in retro.

**Three failed fixes → stop.** Each fix revealing a new problem somewhere else means the design
is the problem, not the line. Write what you tried and what each attempt revealed, and question
the architecture with the user before a fourth attempt.

**Redirect signals.** When the user says "is that really happening?", "you're guessing", "stop,
look again" or similar, you skipped a phase: go back to reproduce or locate.

Then the regression agent proves the fix: it runs your test on the base (must fail) and on the
fix (must pass), and adds no file. Review follows.

## Red flags

| Thought | Reality |
|---|---|
| "Quick fix now, investigate later" | Later never comes; the symptom returns elsewhere. |
| "It's probably X, let me change it" | Probably isn't a probe. Predict, then test. |
| "I'll change these three things at once" | Then you won't know which one mattered. |
| "Add a retry / a sleep / a null check" | That hides the cause. Find where the bad state starts. |
| "One more fix will do it" (after 3) | It's the design. Stop and talk. |
| "Can't reproduce, but this should fix it" | Unverifiable. Get the repro first. |
