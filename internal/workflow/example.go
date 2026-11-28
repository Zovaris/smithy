package workflow

// Example is the shipped example: every day at 17:00 it lists open Dependabot alerts with gh;
// when there are any, an agent judges whether to merge the fixes and the user gets its yes or no
// and why. Set dir to a repo checkout before enabling it.
const Example = `{
  "name": "dependabot-daily",
  "description": "Daily 17:00: open Dependabot alerts; if any, an agent judges whether to merge and you get its yes/no and why.",
  "schedule": "0 17 * * *",
  "dir": "",
  "max_minutes": 20,
  "max_agents": 1,
  "max_cost_usd": 0.5,
  "steps": [
    {"id": "alerts", "kind": "run",
     "run": "gh api 'repos/{owner}/{repo}/dependabot/alerts?state=open' --jq '.[] | \"\\(.security_advisory.severity) \\(.dependency.package.name): \\(.security_advisory.summary)\"'"},
    {"id": "any", "kind": "if", "step": "alerts", "match": "\\S", "else": "stop"},
    {"id": "judge", "kind": "agent", "answer": "yesno", "timeout_minutes": 10,
     "prompt": "These Dependabot alerts are open in this repo:\n\n{{alerts.output}}\n\nLook at the open Dependabot PRs (gh pr list --author app/dependabot) and their CI. Should the user merge them now? Do not merge, push or change anything."},
    {"id": "tell", "kind": "notify", "title": "Dependabot: merge? {{judge.answer}}",
     "message": "{{judge.why}}\n\nOpen alerts:\n{{alerts.output}}"}
  ]
}
`
