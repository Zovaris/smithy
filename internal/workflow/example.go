package workflow

import "sort"

// Examples are the shipped example workflows by name. Set dir to a repo checkout before
// enabling the GitHub ones.
var Examples = map[string]string{
	"dependabot-daily": `{
  "name": "dependabot-daily",
  "description": "Open Dependabot alerts; if any, an agent judges whether to merge and you get its yes/no and why.",
  "dir": "",
  "max_minutes": 20,
  "max_agents": 1,
  "max_cost_usd": 0.5,
  "steps": [
    {"id": "alerts", "kind": "run", "retry": {"count": 2, "backoff": "30s"},
     "run": "gh api 'repos/{owner}/{repo}/dependabot/alerts?state=open' --jq '.[] | \"\\(.security_advisory.severity) \\(.dependency.package.name): \\(.security_advisory.summary)\"'"},
    {"id": "any", "kind": "if", "if": {"step": "alerts", "match": "\\S"}, "else": "stop"},
    {"id": "judge", "kind": "agent", "answer": "yesno", "timeout": "10m",
     "prompt": "These Dependabot alerts are open in this repo:\n\n{{steps.alerts.output}}\n\nLook at the open Dependabot PRs (gh pr list --author app/dependabot) and their CI. Should the user merge them now? Do not merge, push or change anything."},
    {"id": "tell", "kind": "notify", "title": "Dependabot: merge? {{steps.judge.answer}}",
     "message": "{{steps.judge.why}}\n\nOpen alerts:\n{{steps.alerts.output}}"}
  ]
}
`,
	"repo-health": `{
  "name": "repo-health",
  "description": "Failed CI runs and open PRs, summarised by an agent, sent to you.",
  "dir": "",
  "max_minutes": 15,
  "max_agents": 1,
  "max_cost_usd": 0.3,
  "steps": [
    {"id": "ci", "kind": "run", "retry": {"count": 1, "backoff": "20s"},
     "run": "gh run list --status failure --limit 10 --json workflowName,headBranch,createdAt,url"},
    {"id": "prs", "kind": "run", "retry": {"count": 1, "backoff": "20s"},
     "run": "gh pr list --state open --limit 30 --json number,title,isDraft,reviewDecision,updatedAt"},
    {"id": "failed", "kind": "json", "from": "{{steps.ci.output}}", "path": ""},
    {"id": "count", "kind": "set", "vars": {"failures": "{{steps.failed.output}}"}},
    {"id": "summary", "kind": "agent", "timeout": "5m",
     "prompt": "Write this repo's health report for its maintainer: at most 6 short bullets, the most urgent first (red CI on main, PRs waiting on review longest).\n\nFailed CI runs:\n{{vars.failures}}\n\nOpen PRs:\n{{steps.prs.output}}"},
    {"id": "tell", "kind": "notify", "title": "Repo health", "message": "{{steps.summary.output}}"}
  ]
}
`,
	"webhook-reply": `{
  "name": "webhook-reply",
  "description": "An agent's short reply to a message; behind a webhook schedule the reply comes back in the response.",
  "inputs": {"message": ""},
  "outputs": {"reply": "{{steps.reply.output}}"},
  "max_minutes": 5,
  "max_agents": 1,
  "max_cost_usd": 0.1,
  "steps": [
    {"id": "given", "kind": "if", "if": {"value": "{{inputs.message}}", "match": "\\S"}, "else": "stop"},
    {"id": "reply", "kind": "agent", "timeout": "3m",
     "prompt": "Reply in two or three sentences to this message. Change nothing on this machine.\n\n{{inputs.message}}"}
  ]
}
`,
}

// ExampleShipFix is the "ship a fix" example: an agent fixes, the tests loop until green, a
// judge reviews the diff, you approve, and the PR opens.
const exampleShipFix = `{
  "name": "ship-a-fix",
  "description": "An agent fixes the bug you describe, tests loop until green (3 tries), a judge reviews the diff, you approve, then the PR opens.",
  "dir": "",
  "allow_write": true,
  "inputs": {"bug": ""},
  "vars": {"test": "go test ./..."},
  "max_minutes": 45,
  "max_agents": 5,
  "max_cost_usd": 3,
  "steps": [
    {"id": "given", "kind": "if", "if": {"value": "{{inputs.bug}}", "match": "\\S"}, "else": "stop"},
    {"id": "branch", "kind": "run", "run": "git switch -c \"fix/workflow-$(date +%Y%m%d%H%M%S)\""},
    {"id": "green", "kind": "loop", "max": 3, "until": {"step": "test", "exit": 0}, "do": [
      {"id": "fix", "kind": "agent", "write": true, "timeout": "15m",
       "prompt": "Fix this bug in the repo, with a regression test, and commit it: {{inputs.bug}}\n\nThe last test run said:\n{{steps.test.output}}"},
      {"id": "test", "kind": "run", "run": "sh -c \"$SMITHY_VAR_test\"", "timeout": "10m"}
    ]},
    {"id": "diff", "kind": "run", "run": "git diff HEAD~1 --stat && git diff HEAD~1"},
    {"id": "review", "kind": "judge", "subject": "{{steps.diff.output}}", "pass": 0.7,
     "criteria": ["fixes the bug described: {{inputs.bug}}", "adds a test that fails without the fix", "changes nothing unrelated"]},
    {"id": "ok", "kind": "ask", "question": "Open a PR for the fix? Review: {{steps.review.answer}}. {{steps.review.why}}", "options": ["yes", "no"], "timeout": "24h"},
    {"id": "open", "kind": "github", "action": "pr", "when": {"step": "ok", "equals": "yes"},
     "title": "fix: {{inputs.bug}}", "body": "Opened by the ship-a-fix workflow.\n\nReview: {{steps.review.why}}"}
  ]
}
`

func init() { Examples["ship-a-fix"] = exampleShipFix }

// ExampleSchedules attach the examples to their triggers; they are added disabled.
var ExampleSchedules = map[string]string{
	"dependabot-daily": `{"name": "dependabot-daily", "workflow": "dependabot-daily", "description": "Every day at 17:00.", "cron": "0 17 * * *"}`,
	"repo-health":      `{"name": "repo-health", "workflow": "repo-health", "description": "Weekdays at 09:00.", "cron": "0 9 * * 1-5"}`,
	"webhook-reply":    `{"name": "webhook-reply", "workflow": "webhook-reply", "description": "A POST with {\"message\": \"...\"} gets the reply back.", "webhook": true}`,
}

// Example is the Dependabot example.
var Example = Examples["dependabot-daily"]

// ExampleNames are the examples' names, sorted.
func ExampleNames() []string {
	var n []string
	for k := range Examples {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}
