import { api } from "./auth";

export type Ready = { name: string; kind: "cli" | "api"; ready: boolean; detail: string; fix?: string };
export type Choice = { agent: string; model?: string };
export type Kit = { mcp: boolean; skills: string[] };
export type Harness = {
  harness: string; present: boolean; skills_dir: string; smithy: string[]; missing: string[]; other: string[];
  mcp: string[]; mcp_file?: string; mcp_error?: string;
};
export type Setup = { agents: Ready[]; default: Choice; chosen: boolean; kit: Kit; skills: string[]; harnesses: Harness[] };

// choice is what PUT /v1/setup/default gets for row r and a typed model; an API agent needs one.
export function choice(r: Ready, model: string): { agent: string; model: string } | null {
  const m = model.trim();
  if (r.kind === "api") return m ? { agent: r.name + "/" + m, model: "" } : null;
  return { agent: r.name, model: m };
}

export type Model = { id: string; name?: string; default?: boolean; in_per_m?: number; out_per_m?: number };
export type ModelList = { agent: string; models: Model[]; source: string; fetched: string; error?: string };
export type ModelRow = { id: string; label: string; hint: string; other?: boolean; isDefault?: boolean };

const usd = (v: number) => "$" + v.toFixed(2);

// modelRows is what the model select shows for filter f: the agent default first (a CLI, no
// filter), the reported models whose id or name holds f, and Other when none is exactly f.
export function modelRows(l: ModelList | null, f: string, kind: "cli" | "api"): ModelRow[] {
  const q = f.trim().toLowerCase();
  const rows: ModelRow[] = [];
  if (!q && kind === "cli") rows.push({ id: "", label: "agent default", hint: "whatever the agent starts with" });
  let exact = false;
  for (const m of l?.models ?? []) {
    if (!m.id.toLowerCase().includes(q) && !(m.name ?? "").toLowerCase().includes(q)) continue;
    exact ||= m.id.toLowerCase() === q;
    const price = m.in_per_m != null && m.out_per_m != null ? usd(m.in_per_m) + "/" + usd(m.out_per_m) + " per M" : "";
    const hint = [m.name && m.name !== m.id ? m.name : "", price].filter(Boolean).join(" · ");
    rows.push({ id: m.id, label: m.id, hint, ...(m.default ? { isDefault: true } : {}) });
  }
  if (q && !exact) rows.push({ id: f.trim(), label: "Other: " + f.trim(), hint: "use this name as typed", other: true });
  return rows;
}

export async function loadModels(agent: string, refresh = false): Promise<ModelList> {
  const r = await api("/v1/agents/" + encodeURIComponent(agent) + "/models" + (refresh ? "?refresh=1" : ""));
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || "HTTP " + r.status);
  return j;
}

export const isDefault = (r: Ready, d: Choice) => d.agent === r.name || d.agent.startsWith(r.name + "/");

export function toggleSkill(k: Kit, s: string): Kit {
  return { mcp: k.mcp, skills: k.skills.includes(s) ? k.skills.filter((x) => x !== s) : [...k.skills, s] };
}

async function send(method: string, path: string, body: unknown): Promise<any> {
  const r = await api(path, { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || `HTTP ${r.status}`);
  return j;
}

export async function loadSetup(): Promise<Setup> {
  const r = await api("/v1/setup");
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return r.json();
}
export const setDefault = (c: { agent: string; model: string }) => send("PUT", "/v1/setup/default", c);
export const setKit = (k: Kit) => send("PUT", "/v1/setup/kit", k);
export const installSkills = (harness = ""): Promise<{ output: string }> => send("POST", "/v1/setup/skills/install", { harness });
