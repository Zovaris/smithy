# tend

Keeps pull requests mergeable (fixes red CI, rebases after sibling merges, answers review
threads) and, in watch mode, watches the repo: red workflow runs on the default branch, labeled
issues, base branch moves and Dependabot alerts. It never adds scope. Started by you, or by
smelt and warden for their own PRs.

| Command | Does |
|---|---|
| `tend 42 57` | Tend those PRs (numbers, URLs or branches) until green or merged |
| `tend batch <batch>` | Every PR of a warden batch as it opens |
| `tend watch [<scopes>\|all]` | Keep going over `prs`, `issues`, `actions`, `base`, `alerts` |
| `tend watch all --fix` | Same, and open fix PRs for red runs and patch/minor alerts |

Routine work runs on your cheap model, real code fixes on the strong one. Waiting on CI is one
`smithy wait`: no tokens until something happens. A flaky test is rerun once; a second real
failure, an unsafe conflict or a decision that is yours comes back as `needs you <url>: <why>`.
It merges only when told `merge auto`, and into production only with your yes.
