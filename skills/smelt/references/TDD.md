# TDD

## Loop
For each behaviour in the acceptance criteria, in tracer-bullet order:
1. **Red.** One test at an agreed seam; run that file. It fails on an assertion naming the
   missing behaviour (an import, syntax or setup error is not red: fix the test).
2. **Green.** The least code that passes; run the file again.
3. Next behaviour. Never write all tests up front.
4. **Refactor** once green: behaviour unchanged, tests untouched. Untested code: snapshot it
   first and require zero diffs (never update snapshots in the same pass).

Typecheck every few cycles; full suite at the end. Docs-only changes: the seam is the repo's
doc lint, or a grep that the required line exists (review reads the meaning).

## Good tests
- The name or failure message names the production break it catches (no comments).
- Expected values are hand-derived literals, never recomputed the way the code does.
- Through the public seam, not private functions or call order.
- Mock only system boundaries (network, clock, randomness, filesystem, third-party APIs), with
  realistic data; assert results, not that a mock was called.
- One reason to fail; arrange / act / assert top to bottom.
- Bug fix: the regression test fails on the unfixed code first.

## Red flags
| Thought | Reality |
|---|---|
| "Too simple to need a test" | Simple code breaks too; the test takes a minute. |
| "I'll write the tests after" | They pass on the first run and prove nothing. |
| "I'll keep the code I wrote as reference" | Code before its test gets deleted; start from red. |
| "The test is wrong, I'll adjust it to green" | Stop, say why, fix it as its own red step. |
| "Mocking this is easier" | Mock only boundaries, or you test the mock. |
| "Skip the failing test just this once" | `skip`, `only`, `xfail`, loosened asserts and snapshot updates are refused. |

Optional guard: `smithy guard-tests` blocks test-file edits while `.smelt/green` exists (`touch`
it before writing code, remove it for the next test). Wire it as the harness's pre-edit hook if
it has one (JSON on stdin; no input prints usage).
