# Turn repeated work into scripts

Before a one-off command, check the repo's `scripts/SCRIPTS.md`. At the end of a task (never mid-task, not
under hush): ran the same multi-step command twice, or wrote a throwaway script (heredoc,
`python -c`, long pipeline) the project will need again? Propose saving them in one line. On a
yes: the repo's `scripts/`, executable, a usage line, arguments not session values, secrets from
env, errors to stderr with non-zero exit; one line in `scripts/SCRIPTS.md` (ledger's format:
name — what — when). Never re-propose a declined one.

Done when the accepted scripts are saved and listed, or nothing qualified.
