# Grilling

After the brainstorm settled the why and the approach, close the remaining decisions.
Treat the plan as a design tree. Each round:

1. Find the **frontier**: open decisions whose prerequisites are all settled.
2. Ask all of them at once, numbered, each with your recommended answer, worded so "yes" accepts
   it. One line of trade-off when the choice isn't obvious.
3. Anything answerable from the code, docs or the web is a fact, not a decision: look it up and
   state it instead of asking. Wide or external questions go to the `scout` skill when installed
   (`smithy skills scout`; read its SKILL.md and follow it), else a read-only subagent.
4. Challenge vague terms against `docs/GLOSSARY.md`; invent one concrete edge-case scenario per
   risky decision ("a user with two accounts cancels mid-payment: what happens?").
5. Record settled terms in the glossary immediately; note ADR candidates.

Stop when the frontier is empty. Then give a numbered summary of every decision and wait for the
user's confirmation before the spec. Under hush, skip the interview: take your recommendation
for every decision off use-smithy's stop list and list them in the final message; the rest is
`blocked:`.
