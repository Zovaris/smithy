// The composer's `/` commands: smithy's own (they act here) and the installed skills (they go to
// the agent as the start of the message, the way you would type them in the CLI).
export type Cmd = { name: string; hint: string; kind: "action" | "skill" };

export const ACTIONS: Cmd[] = [
  { name: "new", hint: "new session", kind: "action" },
  { name: "stop", hint: "stop the turn", kind: "action" },
  { name: "model", hint: "session model", kind: "action" },
  { name: "effort", hint: "session effort", kind: "action" },
  { name: "skills", hint: "skills and MCP", kind: "action" },
  { name: "theme", hint: "steel, frost or plum", kind: "action" },
  { name: "connect", hint: "servers", kind: "action" },
  { name: "agents", hint: "agents", kind: "action" },
  { name: "board", hint: "board", kind: "action" },
  { name: "decisions", hint: "decisions", kind: "action" },
  { name: "ask", hint: "pin a sample question", kind: "action" },
  { name: "answer", hint: "answer in your words", kind: "action" },
  { name: "serve", hint: "serve on or off", kind: "action" },
  { name: "set", hint: "<key> <value>", kind: "action" },
  { name: "unset", hint: "<key> to default", kind: "action" },
  { name: "keys", hint: "API keys", kind: "action" },
  { name: "doctor", hint: "smithy doctor", kind: "action" },
  { name: "settings", hint: "settings", kind: "action" },
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
