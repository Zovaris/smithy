---
name: refine
description: "User-invoked only: turns the user's messy prompt into a structured mega-prompt (goal, context, constraints, acceptance criteria, output format). Run it only when the user types /refine or names refine; never start it on your own."
argument-hint: "<the rough prompt>"
disable-model-invocation: true
---

# refine

Turn the rough prompt in the arguments (or the user's last message) into one prompt another
session can run without asking anything. Do not do the task itself.

## Flow

1. **Restate** the intent in one or two sentences: what the user wants done and why, as you read it.
2. **Ask** only the questions whose answer changes the prompt: one at a time, with the ask tool
   where the harness has one (two to four options, your pick first), else a short plain question.
   Skip what a sensible default or the repo already answers; say the default in the prompt instead.
   Stop asking once the goal, scope and done-when are clear (usually three questions or fewer).
3. **Research** what the prompt needs and you can find yourself: files, commands, versions,
   conventions in the repo (read-only; scout for anything wider). Facts go in the prompt with
   where they came from; never ask the user a fact you can look up.
4. **Write** the prompt, then end the reply with it in one block fenced as `refined`, exactly:

````
```refined
# Goal
<one paragraph: the outcome, not the steps>

# Context
<what exists, with paths and facts found in step 3>

# Constraints
<rules, limits, what not to touch, the user's answers from step 2>

# Acceptance criteria
- <observable check, one per line>

# Output format
<what the session hands back: PR, file, report, answer shape>
```
````

5. **Offer**: one line after the block: "Copy it, or start a new chat with it." Harnesses that
   read the `refined` block show these as buttons; elsewhere the user copies it.

## Rules

- English headings; the prompt body is in the user's language.
- Every criterion is checkable by someone who was not in this chat.
- No invented facts: unknowns are named under Context as open, with the default to use.
- One block per reply, and only the final prompt goes in it.
