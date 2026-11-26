import { api } from "./auth";
import type { Ready } from "./setup";
import type { Sess } from "./store";

export type Project = { dir: string; name: string };
export type ProjectList = { projects: Project[]; default?: string };
export type DirList = { dir: string; parent?: string; dirs: string[] };

// The daemon's agents.Efforts and agents.EffortFlags: smithy caps effort at medium.
const EFFORT_AGENTS = new Set(["claude", "codex"]);
export const efforts = (agent: string): string[] => (EFFORT_AGENTS.has(agent) ? ["", "low", "medium"] : []);

const RANK: Record<string, number> = { working: 0, needs_you: 1 };

// groupAgents is the Agents page: sessions (working first, then newest), ready CLIs, ready API
// agents, and the missing ones with the command that sets them up.
export function groupAgents(agents: Ready[], sessions: Sess[]) {
  const ss = [...sessions].sort((a, b) => (RANK[a.status] ?? 2) - (RANK[b.status] ?? 2) || b.updated.localeCompare(a.updated));
  return {
    sessions: ss,
    clis: agents.filter((a) => a.ready && a.kind === "cli"),
    apis: agents.filter((a) => a.ready && a.kind === "api"),
    missing: agents.filter((a) => !a.ready),
  };
}

// crumbs is a folder path as clickable parts, root first; Windows drives keep their letter.
export function crumbs(dir: string): { label: string; path: string }[] {
  const win = /^[A-Za-z]:\\/.exec(dir);
  const sep = win ? "\\" : "/";
  const root = win ? win[0] : "/";
  const out = [{ label: root, path: root }];
  let p = root;
  for (const part of dir.slice(root.length).split(sep).filter(Boolean)) {
    p = p === root ? root + part : p + sep + part;
    out.push({ label: part, path: p });
  }
  return out;
}

export const projectOf = (l: ProjectList, dir: string) => l.projects.find((p) => p.dir === dir)?.name ?? dir.split(/[\\/]/).filter(Boolean).pop() ?? dir;

export function startDir(l: ProjectList, remembered: string): string {
  if (remembered && l.projects.some((p) => p.dir === remembered)) return remembered;
  return l.default || l.projects[0]?.dir || "";
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const r = await api(path, body === undefined ? { method } : { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || "HTTP " + r.status);
  return j;
}

export const loadProjects = () => call<ProjectList>("GET", "/v1/projects");
export const addProject = (dir: string, def = false) => call<ProjectList>("POST", "/v1/projects", { dir, default: def });
export const removeProject = (dir: string) => call<ProjectList>("DELETE", "/v1/projects", { dir });
export const listDirs = (dir = "") => call<DirList>("GET", "/v1/fs" + (dir ? "?dir=" + encodeURIComponent(dir) : ""));
