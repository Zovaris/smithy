# Codebase map — `docs/agents/MAP.md`

A short orientation an agent reads instead of exploring the repo. Only facts the code doesn't
make obvious (what a folder is for when its name doesn't say, where tests should drive the
system, what not to touch and why); never what `ls`, the manifest or the README already show.
Each fact is checkable against the code and every path in it exists (`smithy drift` checks);
date it and note the commit it describes.

```markdown
# Map
Updated: YYYY-MM-DD at <short sha>

## Commands
<install, dev, test (single file and full), typecheck, lint, build — exact commands>

## Layout
<top-level folders, one line each: what lives there>

## Entry points
<where execution starts: main, server, CLI, jobs, UI routes — file paths>

## Seams
<the public interfaces modules talk through; where tests should drive the system>

## Data
<stores, schemas, migrations folder, external services and their clients>

## Hot spots
<files that change often or break often (git log --format= --name-only | sort | uniq -c | sort -rn | head)>

## Handle with care
<generated code, vendored code, migrations, anything not to edit by hand, and why>

## Decisions
<links to the ADRs that shape the layout>
```

Budget about 1k tokens (~4 KB): drop a section that has nothing non-obvious to say. Refresh when entry points, layout or commands change; `sync` flags a
map whose paths no longer exist.
