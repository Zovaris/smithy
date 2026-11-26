// The composer's `/` commands: smithy's own (they act here) and the installed skills (they go to
// the agent as the start of the message, the way you would type them in the CLI).
export type Cmd = { name: string; hint: string; kind: "action" | "skill" };

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
  tend: "PRs kept green",
  settings: "settings",
};

export const ACTIONS: Cmd[] = Object.entries(HINTS).map(([name, hint]) => ({ name, hint, kind: "action" }));

// suggest lists the commands matching a message that is only `/word` so far.
export function suggest(input: string, skills: string[]): Cmd[] {
  const m = /^\/([\w-]*)$/.exec(input);
  if (!m) return [];
  const q = m[1].toLowerCase();
  const all = [...ACTIONS, ...skills.map((s) => ({ name: s, hint: "skill", kind: "skill" as const }))];
  return all.filter((c) => c.name.startsWith(q)).concat(all.filter((c) => !c.name.startsWith(q) && c.name.includes(q)));
}

// action is the smithy command a sent message names, if it is one and nothing else.
export function action(input: string): string {
  const m = /^\/([\w-]+)\s*$/.exec(input.trim());
  return m && ACTIONS.some((a) => a.name === m[1]) ? m[1] : "";
}
