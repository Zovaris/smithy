# refine

Turns a rough, messy prompt into a structured mega-prompt: goal, context, constraints,
acceptance criteria and output format. It restates what you want, asks only the questions that
change the result (one at a time), looks up the facts itself, then hands back the prompt.

User-invoked only: type `/refine <your rough prompt>`. Agents never start it on their own.

In the smithy TUI and web, the finished prompt shows two buttons: **copy**, or **new session**
to start a fresh session with it. In other harnesses, copy the `refined` block.
