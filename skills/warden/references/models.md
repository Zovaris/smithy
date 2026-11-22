# Models

No model is hard-coded, and workers don't simply inherit yours. `smithy config get
warden.worker_model` decides:

- `route` (default): `smithy route --file <task file> | --text "<words>" --kind <ship|scout> --speed <speed>`
  per item; launch it on the model of the tier it picks (cheap: small or read-only, mid: most
  features, strong: risky, hard or large). Look at each pick and move it one tier when you know
  better, saying why. A domain noun is not risk: in a secret scanner or a crypto library,
  "secret" or "crypto" in every task says what the product is, not that the task is risky.
- `session`: every worker on the parent session's model.
- Any other value: that model for every worker.

A `model` the user named, for the batch or one item, always wins. The plan line shows each item's
tier and model; `batch.json` stores it as `model`.

Unset tiers print `-`: in the plan line ask the user to name each unset tier (not `judge`, which is optional) and save them with
`smithy config set-model <tier> <name>`. Until then that item uses the session default. Under
hush, or in a session another one launched, don't ask: use the session's model and say so.

Other roles: `smithy consult` uses `judge` (optional, the smartest model), else `strong`; tend runs on `cheap` and hands code fixes to `strong`.

## Effort

Workers run at `agent.effort` (default and cap `medium`; `smithy config get agent.effort`). Through
`smithy agent` it is passed for you. A worker started another way (`references/harness.md`)
gets `--effort <value>` where that launcher takes it; otherwise it runs at the harness default,
and you say so once. Never raise effort for a hard task: pick a stronger tier instead.
