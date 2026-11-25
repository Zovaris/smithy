// The composer's `/` commands: smithy's own (they act here) and the installed skills (they go to
// the agent as the start of the message, the way you would type them in the CLI).
export type Cmd = { name: string; hint: string; kind: "action" | "skill" };

export const ACTIONS: Cmd[] = [
  { name: "new", hint: "start a new session", kind: "action" },
  { name: "stop", hint: "stop this session's turn", kind: "action" },
  { name: "agents", hint: "every agent and session", kind: "action" },
  { name: "board", hint: "sessions by state", kind: "action" },
  { name: "decisions", hint: "what waits on you", kind: "action" },
  { name: "settings", hint: "agents, model, skills, MCP", kind: "action" },
  { name: "theme", hint: "light or dark", kind: "action" },
];

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
