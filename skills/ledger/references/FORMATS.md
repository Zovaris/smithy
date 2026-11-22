# ADR format

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
