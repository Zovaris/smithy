import { STATUS_CLASS, STATUS_WORD, type Status } from "../lib/store";

export const St = ({ s, word }: { s: Status | "idle" | "ready" | "missing"; word?: string }) => {
  const cls = s === "idle" || s === "missing" ? "idle" : s === "ready" ? "done" : STATUS_CLASS[s];
  return <span class={"st " + cls}>{word ?? (s === "idle" || s === "ready" || s === "missing" ? s : STATUS_WORD[s])}</span>;
};

// command is the shell command or host a guard question names, shown as code under the title.
export function split(q: string): { title: string; code: string } {
  let m = /^Run this command\? ([\s\S]+)$/.exec(q);
  if (m) return { title: "Run a command that matches the guard?", code: m[1] };
  m = /^Fetch from (.+)\?$/.exec(q);
  if (m) return { title: "Fetch from a host the guard blocks?", code: m[1] };
  return { title: q, code: "" };
}

const LABEL: Record<string, string> = { "run once": "Run once", "always allow this pattern": "Always allow", deny: "Deny" };
export const label = (o: string) => LABEL[o] ?? o.charAt(0).toUpperCase() + o.slice(1);
