// What the web needs to do what the TUI does: the effort and model rules, what a composer line
// means (`!cmd`, a smithy command, or a message), images in a message, typed settings and the
// header line.
import { ACTIONS } from "./commands";

// EFFORT_AGENTS are the CLIs that take an effort flag (agents.EffortFlags); smithy caps at medium.
const EFFORT_AGENTS = ["claude", "codex"];
export const EFFORTS = ["low", "medium"];

export function effortOptions(agent: string): string[] {
  return EFFORT_AGENTS.includes(agent.replace(/^acp:/, "")) ? ["", ...EFFORTS] : [];
}

export const modelEditable = (agent: string) => !agent.startsWith("api:");

export type Intent = { kind: "shell"; cmd: string } | { kind: "action"; name: string; arg: string } | { kind: "send"; text: string };

export function intent(input: string): Intent {
  const t = input.trim();
  if (t.startsWith("!") && t.slice(1).trim()) return { kind: "shell", cmd: t.slice(1).trim() };
  const m = /^\/([\w-]+)(?:\s+([\s\S]*))?$/.exec(t);
  if (m && ACTIONS.some((a) => a.name === m[1])) return { kind: "action", name: m[1], arg: (m[2] ?? "").trim() };
  return { kind: "send", text: t };
}

export function withImages(text: string, paths: string[]): string {
  return paths.length ? text + "\n\nAttached images:" + paths.map((p) => "\n" + p).join("") : text;
}

export type SettingRow = { key: string; value: string; src: string; default: string; meaning: string; kind: string; options?: string[]; secret?: boolean };
export type Control = "toggle" | "select" | "number" | "list" | "text" | "secret";

export function controlOf(r: SettingRow): Control {
  if (r.secret) return "secret";
  if (r.kind === "bool") return "toggle";
  if (r.kind === "enum") return "select";
  if (r.kind === "number" || r.kind === "list") return r.kind;
  return "text";
}

export function groupSettings(rows: SettingRow[]): { group: string; rows: SettingRow[] }[] {
  const out: { group: string; rows: SettingRow[] }[] = [];
  for (const r of rows) {
    const g = r.key.split(".")[0];
    const at = out.find((x) => x.group === g);
    if (at) at.rows.push(r);
    else out.push({ group: g, rows: [r] });
  }
  return out;
}

export function headerLine(version: string, dir: string): string {
  const v = version ? " " + version.replace(/^(?=\d)/, "v") : "";
  const p = dir.split("/").filter(Boolean).pop() ?? "";
  return "◆ smithy" + v + (p ? " · " + p : "");
}

export const clock = (d: Date) => String(d.getHours()).padStart(2, "0") + ":" + String(d.getMinutes()).padStart(2, "0");
