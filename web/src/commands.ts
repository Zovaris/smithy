// The composer's `/` commands: smithy's own (they act here) and the installed skills (they go to
// the agent as the start of the message, the way you would type them in the CLI).
export type Cmd = { name: string; hint: string; kind: "action" | "skill" };

export const ACTIONS: Cmd[] = [
  { name: "new", hint: "start a new session", kind: "action" },
  { name: "stop", hint: "stop this session's turn", kind: "action" },
  { name: "model", hint: "the model this session runs", kind: "action" },
  { name: "effort", hint: "this session's reasoning effort", kind: "action" },
  { name: "skills", hint: "skills and MCP injected into every session", kind: "action" },
  { name: "theme", hint: "steel, frost or plum", kind: "action" },
  { name: "connect", hint: "servers: add one, switch", kind: "action" },
  { name: "agents", hint: "every agent and session", kind: "action" },
  { name: "board", hint: "sessions by state", kind: "action" },
  { name: "decisions", hint: "what waits on you", kind: "action" },
  { name: "ask", hint: "pin a sample question, to try decisions", kind: "action" },
  { name: "answer", hint: "answer the open decision in your words", kind: "action" },
  { name: "serve", hint: "serve on or off (auto.paused)", kind: "action" },
  { name: "set", hint: "save a setting: /set <key> <value>", kind: "action" },
  { name: "unset", hint: "put a setting back to its default", kind: "action" },
  { name: "keys", hint: "stored API keys, masked", kind: "action" },
  { name: "doctor", hint: "run smithy doctor", kind: "action" },
  { name: "settings", hint: "every setting, keys, servers", kind: "action" },
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
