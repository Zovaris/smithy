# Builder check

Read-only. You are the engineer who will build this spec tomorrow with no one to ask. Find what
would stop you or make you guess. Use only file reading and searching; don't edit anything. Never check out or switch branches (other sessions share this
checkout); read another ref with `git show <ref>:<file>`.

## You get
- The spec path (and the task list, if written).
- The repo, to check claims against the code.

## Do
1. Read the spec as a builder: for each behaviour, could you write the first failing test now?
2. List every open question you hit: undefined terms, missing edge cases, unclear data shapes,
   conflicts with existing code, unstated non-goals.
3. Sort each one:
   - **answered**: the spec, the code or the docs already answer it. Quote where.
   - **minor**: a reasonable engineer would just pick; give the pick and why.
   - **theirs**: changes behaviour, cost, data or an interface the user cares about; give the
     options and your recommendation.

## Report (≤300 words)
```
answered:
- <question> → <where it's answered>
minor:
- <question> → <pick> (why)
theirs:
- <question> → options: <a / b> · recommend: <a> (why)
```
Nothing open is a valid result.
