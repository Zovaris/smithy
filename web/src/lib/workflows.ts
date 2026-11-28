// Workflows (`smithy workflow`): listed, run, turned on or off, validated, edited and their runs
// read through the daemon.
import { api } from "./auth";

export type Cond = { value?: string; step?: string; exit?: number; match?: string; equals?: string; path?: string; op?: string; to?: string };
export type Step = {
  id: string;
  kind: string;
  when?: Cond;
  run?: string;
  prompt?: string;
  answer?: string;
  if?: Cond;
  then?: string;
  else?: string;
  value?: string;
  vars?: Record<string, string>;
  from?: string;
  path?: string;
  items?: string;
  do?: Step[];
  question?: string;
  title?: string;
  message?: string;
  workflow?: string;
  subject?: string;
  criteria?: string[];
  until?: Cond;
  max?: number;
  task?: string;
  method?: string;
  url?: string;
  branches?: Step[][];
  for?: string;
  check?: string;
  action?: string;
  pr?: string;
  op?: string;
  code?: string;
};
export type StepResult = {
  id: string;
  kind: string;
  status: string;
  duration_ms: number;
  attempts?: number;
  input?: string;
  output?: string;
  exit: number;
  answer?: string;
  why?: string;
  branch?: string;
  error?: string;
  cost_usd?: number;
  steps?: StepResult[];
};
export type RunRecord = { workflow: string; trigger?: string; schedule?: string; inputs?: Record<string, string>; outputs?: Record<string, string>; started: string; ended?: string; status: string; error?: string; cost_usd?: number; steps: StepResult[] };
export type Workflow = {
  name: string;
  description?: string;
  inputs?: Record<string, string>;
  steps: Step[];
  file: string;
  schedules?: string[];
  last?: RunRecord;
  error?: string;
};
export type Schedule = {
  name: string;
  description?: string;
  workflow: string;
  cron?: string;
  webhook?: boolean;
  github?: { event: string; label?: string };
  watch?: string;
  file: string;
  enabled: boolean;
  changed?: boolean;
  next?: string;
  last?: RunRecord;
  error?: string;
};
export type ScheduleDetail = { info: Schedule; source: string; history: RunRecord[]; hook?: string };
export type WorkflowList = { workflows: Workflow[]; running: string[] };
export type WorkflowDetail = { info: Workflow; source: string; history: RunRecord[]; running: boolean; live?: RunRecord };

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const r = await api(path, init);
  const b = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(b.error || `HTTP ${r.status}`);
  return b as T;
}

const json = (method: string, body: string): RequestInit => ({ method, headers: { "Content-Type": "application/json" }, body });
const at = (name: string) => "/v1/workflows/" + encodeURIComponent(name);

export const loadWorkflows = () => call<WorkflowList>("/v1/workflows");
export const loadWorkflow = (name: string) => call<WorkflowDetail>(at(name));
export const runWorkflow = (name: string, inputs: Record<string, string> = {}) => call<{ status: string }>(at(name) + "/run", json("POST", JSON.stringify({ inputs })));
const sat = (name: string) => "/v1/schedules/" + encodeURIComponent(name);
export const loadSchedules = () => call<{ schedules: Schedule[] }>("/v1/schedules");
export const loadSchedule = (name: string) => call<ScheduleDetail>(sat(name));
export const setSchedule = (name: string, on: boolean) => call<Schedule>(sat(name) + (on ? "/enable" : "/disable"), json("POST", "{}"));
export const saveWorkflow = (name: string, source: string) => call<Workflow>(at(name), json("PUT", source));
export const validateWorkflow = (source: string) => call<{ ok: boolean; error?: string }>("/v1/workflows/validate", json("POST", source));

// state is a schedule's one-word state, the way the TUI and `smithy schedule list` name it.
export function state(s: Schedule): "error" | "changed" | "on" | "off" {
  if (s.error) return "error";
  if (s.changed) return "changed";
  return s.enabled ? "on" : "off";
}

// trigger names what starts a schedule.
export const trigger = (s: Schedule) => s.cron || (s.webhook ? "webhook" : s.github ? `github ${s.github.event}${s.github.label ? " " + s.github.label : ""}` : `watch ${s.watch ?? ""}`);

const one = (t = "", n = 90) => {
  const x = t.split(/\s+/).filter(Boolean).join(" ");
  return x.length > n ? x.slice(0, n) + "…" : x;
};

// summary is a step in one line, like the TUI's steps list.
export function summary(s: Step): string {
  const c = (x?: Cond) => (x ? `${x.value || x.step} ${x.exit != null ? "exit " + x.exit : x.match ? "~ /" + x.match + "/" : x.equals != null ? "== " + x.equals : `.${x.path} ${x.op || "eq"} ${x.to ?? ""}`}` : "");
  const by: Record<string, string> = {
    run: s.run ?? "",
    agent: (s.answer ? `(${s.answer}) ` : "") + (s.prompt ?? ""),
    if: `${c(s.if)} → ${s.then || "next"}, else ${s.else || "next"}`,
    switch: s.value ?? "",
    set: Object.keys(s.vars ?? {}).join(", "),
    json: `${s.path} of ${s.from}`,
    foreach: `${s.items}: ${s.do?.length ?? 0} steps`,
    ask: s.question ?? "",
    notify: `${s.title ?? ""}: ${s.message ?? ""}`,
    call: s.workflow ?? "",
    judge: `${s.subject} against ${s.criteria?.length ?? 0} criteria`,
    loop: `${s.do?.length ?? 0} steps until ${c(s.until)}`,
    session: s.prompt ?? "",
    smelt: s.task ?? "",
    warden: s.task ?? "",
    http: `${(s.method || "GET").toUpperCase()} ${s.url}`,
    parallel: `${s.branches?.length ?? 0} branches`,
    wait: s.for || s.check || "",
    github: `${s.action} ${s.pr ?? ""}`.trim(),
    file: `${s.op} ${s.path}`,
    code: s.code ?? "",
  };
  if (s.kind === "agent" && s.items) by.agent = `each of ${s.items}: ${by.agent}`;
  return one((by[s.kind] ?? "") + (s.when ? ` (when ${c(s.when)})` : ""), 100);
}

// result is one step result as a line: skipped, its error, branch, answer or exit code and output.
export function result(s: StepResult): string {
  if (s.status === "skipped") return "skipped";
  if (s.error) return "error: " + one(s.error);
  if (s.branch) return "→ " + s.branch;
  if (s.answer) return one(s.answer + " " + (s.why ?? ""));
  if (s.kind === "run") return `exit ${s.exit} · ${one(s.output)}`;
  return one(s.output);
}

// opensSession says whether a step's output is the id of the chat session it started.
export const opensSession = (s: StepResult) => ["session", "smelt", "warden"].includes(s.kind) && !!s.output && !s.error;

// ms is a step's duration, short.
export const ms = (n: number) => (n < 1000 ? `${n}ms` : n < 60000 ? `${(n / 1000).toFixed(1)}s` : `${Math.round(n / 60000)}m`);
