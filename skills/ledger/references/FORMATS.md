# ADR and glossary formats

## ADR

Lean, with section names that match [MADR](https://github.com/adr/madr), so tools like
log4brains and adr-manager can read the files. Sections marked optional are left out when they
add nothing; an ADR can be one paragraph.

```markdown
# NNNN · <title in imperative or noun form>

- Status: proposed | accepted | rejected | deprecated | superseded by NNNN
- Date: YYYY-MM-DD

## Context and Problem Statement

<What forced a decision, in one or two paragraphs. Facts and constraints, with links.>

## Considered Options

<optional>
- <option> — <why not, in one line>

## Decision Outcome

Chosen option: "<option>", because <the reason that tipped it>.

## Consequences

<optional>
- Good: <what gets easier>
- Bad: <what gets harder, what we accept>
```

Rules:
- Number with `smithy adr`; never reuse a number, even for rejected ADRs.
- Status changes are the only edit allowed after `accepted`. A changed decision is a new ADR that
  supersedes the old one.
- Link the PR or issue where the decision was made instead of copying the discussion.

## Glossary

`docs/GLOSSARY.md`, one line per term, alphabetical, in domain language:

```markdown
# Glossary

**<Term>** — <one-sentence meaning a domain expert would agree with>. _Not:_ <synonyms we avoid>.
```

Rules:
- No implementation detail (no class names, tables, endpoints).
- One term, one meaning. If the code uses two names for one concept, pick one here and list the
  other under _Not_.
- Add or fix a term the moment it is settled in a conversation, not at the end.
- When code names disagree with the glossary, flag it; renaming is a separate decision.
