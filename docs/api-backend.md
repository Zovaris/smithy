# API backend and benchmark (harness build)

## API backend

`smithy agent --backend api --model <provider>/<model>` runs smithy's own agent loop on your own
API key instead of driving a CLI. For `smithy auto serve`, set `auto.agent` to
`api:<provider>/<model>`.

```
smithy auth set anthropic < keyfile
smithy agent --backend api --model anthropic/<model> prompt.md
smithy config set auto.agent api:openai/<model>
```

| Provider | Key (`smithy auth set <name>`) |
|---|---|
| `anthropic` | `anthropic` |
| `openai` | `openai` |
| `gemini` | `gemini` |
| `openrouter` | `openrouter` |
| `deepseek` | `deepseek` |
| `ollama` | none (local, `localhost:11434`) |

- Keys are read from `~/.smithy/auth.json`, encrypted at rest with `~/.smithy/auth.key` next to
  it. Any process running as you can read both unless the optional OS sandbox is on.
- The loop has read, write, edit, shell, glob, grep and fetch tools; every call goes through the
  `--profile auto` rules. `--readonly` runs cannot change files.
- `auto.sandbox` is off by default; with `native`, the loop's shell runs inside the OS sandbox.
- Code mode (`agent.code_mode`, off by default) adds `run_code`: one JavaScript program that
  calls the loop's tools, each still permission-checked (QuickJS in WebAssembly, 64 MB, 30 s, no
  files or network). Turn it on only if `smithy bench` shows it helps.

## Benchmark

`smithy bench` runs smithy's own task set: small Go repos whose tests decide pass or fail. Each
task runs in a fresh git repo in a temp dir, through `smithy agent --profile auto`.

```
smithy bench run --agent claude          # or api:<provider>/<model>
smithy bench list                        # the tasks
smithy bench runs                        # saved runs
smithy bench compare latest~1 latest     # pass rate, time, tokens, cost, regressions
```

Runs are saved in `~/.smithy/bench/`; the web UI's Bench screen shows the same comparison. Every
flag: `smithy help bench`.
