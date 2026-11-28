// Scheduled workflows (`smithy workflow`): listed, run, turned on or off, edited and their run
// history read through the daemon.
import { api } from "./auth";

export type Step = { id: string; kind: "run" | "agent" | "if" | "notify"; run?: string; prompt?: string; answer?: string; step?: string; title?: string; message?: string };
export type StepResult = { id: string; kind: string; exit: number; output?: string; answer?: string; why?: string; branch?: string; error?: string; cost_usd?: number };
export type RunRecord = { workflow: string; trigger?: string; started: string; ended?: string; status: string; error?: string; cost_usd?: number; steps: StepResult[] };
export type Workflow = {
  name: string;
  description?: string;
  schedule: string;
  steps: Step[];
  file: string;
  enabled: boolean;
  changed?: boolean;
  next?: string;
  last?: RunRecord;
  error?: string;
};
export type WorkflowList = { workflows: Workflow[]; running: string[] };
export type WorkflowDetail = { info: Workflow; source: string; history: RunRecord[]; running: boolean };

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const r = await api(path, init);
  const b = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(b.error || `HTTP ${r.status}`);
  return b as T;
}

const post = (body = "{}"): RequestInit => ({ method: "POST", headers: { "Content-Type": "application/json" }, body });
const at = (name: string) => "/v1/workflows/" + encodeURIComponent(name);

export const loadWorkflows = () => call<WorkflowList>("/v1/workflows");
export const loadWorkflow = (name: string) => call<WorkflowDetail>(at(name));
export const runWorkflow = (name: string) => call<{ status: string }>(at(name) + "/run", post());
export const setWorkflow = (name: string, on: boolean) => call<Workflow>(at(name) + (on ? "/enable" : "/disable"), post());
export const saveWorkflow = (name: string, source: string) =>
  call<Workflow>(at(name), { method: "PUT", headers: { "Content-Type": "application/json" }, body: source });

// state is a workflow's one-word state, the way the TUI and `smithy workflow list` name it.
export function state(w: Workflow, running: string[]): "running" | "error" | "changed" | "on" | "off" {
  if (running.includes(w.name)) return "running";
  if (w.error) return "error";
  if (w.changed) return "changed";
  return w.enabled ? "on" : "off";
}

// result is one step result as a line: its error, branch, answer or exit code and output.
export function result(s: StepResult): string {
  const one = (t = "", n = 90) => {
    const x = t.split(/\s+/).filter(Boolean).join(" ");
    return x.length > n ? x.slice(0, n) + "…" : x;
  };
  if (s.error) return "error: " + one(s.error);
  if (s.kind === "if") return "→ " + (s.branch ?? "next");
  if (s.answer) return (s.answer + " " + one(s.why)).trim();
  if (s.kind === "run") return `exit ${s.exit} · ${one(s.output)}`;
  return one(s.output);
}
