// The composer's `/` commands: smithy's own (they act here) and the installed skills (they go to
// the agent as the start of the message, the way you would type them in the CLI).
// fill, when set, is the text picking the row puts in the composer.
export type Cmd = { name: string; hint: string; kind: "action" | "skill"; fill?: string };

// Each command and its hint, in the order the `/` menu lists them.
const HINTS: Record<string, string> = {
  new: "new session",
  sessions: "agents and sessions",
  agent: "default agent",
  stop: "stop the turn",
  model: "session model",
  effort: "session effort",
  skills: "skills and MCP",
  theme: "steel, frost, plum, light or dark",
  mode: "transcript detail",
  connect: "servers",
  remote: "from another machine",
  agents: "agents",
  board: "board",
  decisions: "decisions",
  bench: "bench",
  log: "daemon log",
  welcome: "first-run steps",
  setup: "agents ready or missing",
  ask: "pin a sample question",
  answer: "answer in your words",
  help: "keys and commands",
  serve: "serve on or off",
  set: "<key> <value>",
  unset: "<key> to default",
  keys: "API keys",
  key: "rm <service>",
  doctor: "smithy doctor",
  prompt: "system prompt file",
  tend: "PRs kept green",
  channels: "chat channels, test-send",
  settings: "settings",
};

export const ACTIONS: Cmd[] = Object.entries(HINTS).map(([name, hint]) => ({ name, hint, kind: "action" }));

// skillDir is where a skill lives, from its SKILL.md path, the home folder shown as ~ like the TUI.
const skillDir = (p: string) => p.replace(/\/[^/]+\/[^/]+$/, "").replace(/^\/(?:Users|home)\/[^/]+(?=\/)/, "~");

// match keeps the rows whose name holds q (or, from three letters on, whose hint does), name-prefix
// matches first.
function match(l: Cmd[], q: string): Cmd[] {
  const hit = (c: Cmd) => c.name.includes(q) || (q.length >= 3 && c.hint.toLowerCase().includes(q));
  return l.filter((c) => c.name.startsWith(q)).concat(l.filter((c) => !c.name.startsWith(q) && hit(c)));
}

// suggest lists what a message that is only `/word` so far can be, as the TUI's / menu does: the
// commands, then the skills (marked as skills, with where each lives).
export function suggest(input: string, skills: string[], paths: Record<string, string> = {}): Cmd[] {
  const m = /^\/([\w-]*)$/.exec(input);
  if (!m) return [];
  const q = m[1].toLowerCase();
  const own = skills.filter((s) => !HINTS[s]).map((s): Cmd => ({ name: s, hint: paths[s] ? skillDir(paths[s]) : "skill", kind: "skill" }));
  return [...match(ACTIONS, q), ...match(own, q)];
}

// action is the smithy command a sent message names, if it is one and nothing else.
export function action(input: string): string {
  const m = /^\/([\w-]+)\s*$/.exec(input.trim());
  return m && ACTIONS.some((a) => a.name === m[1]) ? m[1] : "";
}
