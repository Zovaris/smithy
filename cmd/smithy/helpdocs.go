package main

// Help groups, in the order `smithy help` lists them.
const (
	groupStart    = "Start here"
	groupDaily    = "Daily"
	groupAccounts = "Accounts and secrets"
	groupMaint    = "Maintenance"
	groupSkills   = "Used by the skills"
	groupHidden   = ""
)

var groupOrder = []string{groupStart, groupDaily, groupAccounts, groupMaint, groupSkills}

// helpDoc is what `smithy help <command>` adds around a command's usage text.
type helpDoc struct {
	group    string
	examples []string
	see      []string
}

// harnessSummary is the one-line help of each harness-only command, shared by both builds.
var harnessSummary = map[string]string{
	"daemon": "run the auto loop with a local API (events, decisions)",
	"attach": "watch a daemon live: events, decisions, reconnect and resume",
	"chat":   "fullscreen chat over a daemon (also bare smithy, which starts the daemon)",
	"stop":   "stop the local background daemon",
	"web":    "print or open the daemon's web UI link",
	"board":  "built-in tracker: add, list, show, set, claim, sync",
	"mcp":    "MCP server (stdio) for driven CLIs: board, ask, secret calls, search",
}

var helpDocs = map[string]helpDoc{
	"setup":   {groupStart, []string{"smithy setup status", "smithy setup block add"}, []string{"doctor", "config"}},
	"doctor":  {groupStart, []string{"smithy doctor", "smithy doctor --quiet", "smithy doctor --fix"}, []string{"setup", "version"}},
	"version": {groupStart, []string{"smithy version", "smithy version --verbose"}, []string{"doctor"}},
	"status":  {groupStart, []string{"smithy status"}, []string{"tasks", "cards"}},

	"chat":    {groupDaily, []string{"smithy chat", "smithy chat --standalone", "smithy chat http://127.0.0.1:7477"}, []string{"stop", "daemon", "attach"}},
	"stop":    {groupDaily, []string{"smithy stop"}, []string{"chat", "daemon"}},
	"daemon":  {groupDaily, []string{"smithy daemon", "smithy daemon --bg", "smithy daemon --once"}, []string{"chat", "stop", "web", "auto"}},
	"attach":  {groupDaily, []string{"smithy attach"}, []string{"daemon", "chat"}},
	"web":     {groupDaily, []string{"smithy web", "smithy web open"}, []string{"daemon"}},
	"board":   {groupDaily, []string{`smithy board add "Fix login" --priority 1`, "smithy board list --state todo", "smithy board show B-1"}, []string{"auto", "tasks"}},
	"bench":   {groupMaint, []string{"smithy bench list", "smithy bench run --agent claude --skills v0.22.0", "smithy bench run --agent codex --task bug-sum-last"}, []string{"agent", "receipt"}},
	"report":  {groupDaily, []string{"smithy report week"}, []string{"tui", "decisions"}},
	"tui":     {groupDaily, []string{"smithy tui", "smithy tui --no-anim"}, []string{"auto", "report"}},
	"auto":    {groupDaily, []string{"smithy auto serve", "smithy auto status", "smithy auto poll --source github"}, []string{"config", "tui", "notify"}},
	"tasks":   {groupDaily, []string{"smithy tasks board --all", "smithy tasks next"}, []string{"status"}},
	"wait":    {groupDaily, []string{"smithy wait pr:42", "smithy wait pr:42 run:123 --all"}, []string{"status"}},
	"receipt": {groupDaily, []string{"smithy receipt run -- go test ./...", `smithy receipt run --claim "tests pass" -- npm test`, "smithy receipt list --md"}, []string{"scan", "verify"}},
	"scan":    {groupDaily, []string{"smithy scan", "smithy scan --base main"}, []string{"receipt"}},
	"search":  {groupDaily, []string{`smithy search "retry policy"`, `smithy search "auth" --adr`}, []string{"adr", "drift"}},

	"auth":   {groupAccounts, []string{"smithy auth set linear < key.txt", "smithy auth list", "smithy auth remove linear"}, []string{"pool", "config"}},
	"pool":   {groupAccounts, []string{"smithy pool add work claude", "smithy pool login work", "smithy pool list"}, []string{"agent", "auth"}},
	"config": {groupAccounts, []string{"smithy config list", "smithy config get auto.sources", "smithy config set-model cheap haiku"}, []string{"setup", "auth"}},
	"notify": {groupAccounts, []string{"smithy notify status", "smithy notify test"}, []string{"auth", "auto"}},

	"cleanup":   {groupMaint, []string{"smithy cleanup . feat/42-login", "smithy cleanup . feat/42-login ../wt/login --force"}, []string{"worktree"}},
	"uninstall": {groupMaint, []string{"smithy uninstall", "smithy uninstall --yes"}, []string{"cleanup", "skills"}},
	"skills":    {groupMaint, []string{"smithy skills --list", "smithy skills smelt"}, []string{"setup"}},

	"adr":         {groupSkills, []string{`smithy adr "Use SQLite for the board"`}, []string{"search"}},
	"agent":       {groupSkills, []string{"smithy agent prompt.md", "smithy agent --model sonnet --timeout 30 prompt.md", "smithy agent --backend api --model anthropic/<model> prompt.md"}, []string{"pool", "route"}},
	"batch":       {groupSkills, []string{"smithy batch init ./tasks --repo ."}, []string{"brief", "cards"}},
	"brief":       {groupSkills, []string{"smithy brief skills-v021 v021-01"}, []string{"batch"}},
	"cards":       {groupSkills, []string{"smithy cards set my-batch t1 state=working", "smithy cards beat my-batch t1"}, []string{"status", "batch"}},
	"changed":     {groupSkills, []string{"smithy changed", "smithy changed main --csv"}, []string{"stack"}},
	"comments":    {groupSkills, []string{"smithy comments", "smithy comments --diff main"}, []string{"drift"}},
	"commit":      {groupSkills, []string{"smithy commit style", `smithy commit "fix: handle empty input" main.go`}, []string{"pr"}},
	"consult":     {groupSkills, []string{`smithy consult "Which cache?" "redis | memory"`}, []string{"decisions", "decide"}},
	"decide":      {groupSkills, []string{"smithy decide rules --base main", `smithy decide impact "Drop the v1 API?"`}, []string{"consult"}},
	"guard":       {groupSkills, []string{`smithy guard check "rm -rf /"`, "smithy guard list"}, []string{"agent", "config"}},
	"decisions":   {groupSkills, []string{"smithy decisions pending", "smithy decisions confirm --all"}, []string{"consult", "report"}},
	"drift":       {groupSkills, []string{"smithy drift", "smithy drift main"}, []string{"comments", "search"}},
	"guard-tests": {groupSkills, []string{"smithy guard-tests internal/x/x_test.go"}, []string{"stack"}},
	"harness":     {groupSkills, []string{"smithy harness"}, []string{"config"}},
	"mcp":         {groupSkills, []string{"smithy mcp", "smithy mcp --readonly"}, []string{"agent", "search", "board"}},
	"names":       {groupSkills, []string{`smithy names branch 42 "Add login"`, "smithy names worktree feat/42-add-login"}, []string{"worktree"}},
	"pr":          {groupSkills, []string{"smithy pr template", `smithy pr create --base main --title "Add login" --body-file body.md`}, []string{"commit", "pr-state"}},
	"pr-state":    {groupSkills, []string{"smithy pr-state 42"}, []string{"pr", "wait"}},
	"proxy":       {groupSkills, []string{"smithy proxy serve --allow api.github.com", "smithy proxy approve 3"}, []string{"auth", "agent"}},
	"route":       {groupSkills, []string{"smithy route --file task.md", `smithy route --text "fix typo in README" --json`}, []string{"agent", "config"}},
	"stack":       {groupSkills, []string{"smithy stack", "smithy stack ../other-repo"}, []string{"changed"}},
	"task-dir":    {groupSkills, []string{"smithy task-dir add-login"}, []string{"tasks"}},
	"verify":      {groupSkills, []string{"smithy verify . feat/42-login 42"}, []string{"receipt", "pr-state"}},
	"wait-turn":   {groupSkills, []string{"smithy wait-turn my-batch t2"}, []string{"cards", "wait"}},
	"worktree":    {groupSkills, []string{"smithy worktree add . feat/42-login main"}, []string{"names", "cleanup"}},

	"engine-probe": {groupHidden, []string{"smithy engine-probe"}, nil},
}
