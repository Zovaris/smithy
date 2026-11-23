# hush

Does the task with no talk. No narration between steps, no questions; progress shows in your
harness's task list, and the session ends with one line:

```
done: PR https://github.com/acme/app/pull/42
blocked: the migration needs a production backup first
```

Started by you (by name, the way your harness starts skills) or by an agent. It composes with other skills: hush + smelt build next.

- Low-impact choices it makes itself and lists where the work lands (PR body or final line).
- It commits or opens a PR only when the task asks for it.
- Your decisions: the hard floor (merging or deploying to the production branch, deleting or irreversibly rewriting user data, credentials, secrets or logins, money) stops with `blocked: <why>`; the rest of the stop list goes to one `smithy consult` and
  is logged with `smithy decisions add` for you to keep or change.
