# Brainstorm

Find out what the user actually wants, and why, before designing anything. Your belief about
what they want is usually thinner than it feels.

## 0 · What do you already know?

- **Everything that matters is in the request** ("make the icon blue"): skip to the build phase at `fast` speed.
- **Something non-trivial is missing:** continue.

Look before you ask: read what the user pointed at and the code it touches until you can say
how it works today and what the change would touch. Questions about libraries, APIs or other
systems go to the `scout` skill when installed (`smithy skills scout`), else a read-only
subagent. Never ask what the project already answers
or reopen what they already decided.

## 1 · The why

First message, short (~200 words):
- They said what they want: echo it back in your words (what, for whom, why so far), add what
  you found that bears on it (how it works today, constraints, a cheaper path), and invite
  corrections. If they haven't said why, ask that: one open question, in their terms.
- They haven't: one open question about the biggest unsettled thing, saying what the answer
  would change.

One question per message here; this is a conversation, not a form.

## 2 · Approaches

Once the why is clear, a message of its own: 2–3 concrete approaches, each with what it trades
off, and which you'd pick and why. The user picks. If there is truly one sensible way, say so,
in one line on why the others don't fit.

## 3 · Design

Write the design for the chosen approach, sized to the work: a few lines for small things, a
short doc in sections for big ones (behaviour, data, edge cases, what's out). Show it, get
approval. It becomes the top of `spec.md`.

Then grill the remaining open decisions (the plan phase's next step) and write the spec and tasks.

## `/smelt brainstorm`

Stops after step 3: the design is saved (`spec.md` with `Status: draft`) and nothing is split or
built. Under hush: skip the questions, pick the approach yourself (unless it is on use-smithy's stop list: `blocked:`) and list what you assumed.
