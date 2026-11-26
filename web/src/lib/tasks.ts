// The Board's data: a project's local task files (`smithy tasks`), read and moved through the
// daemon. A card's column is its Status line; moving it rewrites that line in the file.
import { api } from "./auth";

export type TaskCard = { file: string; nn: string; title: string; status: string; pr?: string; blockers: string[] };
export type TaskList = { dir: string; name: string; tasks: TaskCard[] };
export type TaskBoard = { project: string; statuses: string[]; lists: TaskList[] };

export const COLUMNS = [
  { id: "todo", label: "Todo", empty: "Nothing to start." },
  { id: "doing", label: "Doing", empty: "Nothing in progress." },
  { id: "done", label: "Done", empty: "Nothing finished yet." },
  { id: "dropped", label: "Dropped", empty: "Nothing dropped." },
];

export async function loadTasks(project = ""): Promise<TaskBoard> {
  const r = await api("/v1/tasks" + (project ? "?project=" + encodeURIComponent(project) : ""));
  if (!r.ok) throw new Error(`tasks: HTTP ${r.status}`);
  return r.json();
}

export async function moveTask(file: string, status: string): Promise<TaskCard> {
  const r = await api("/v1/tasks/status", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ file, status }) });
  const b = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(b.error || `HTTP ${r.status}`);
  return b;
}

// moved is the board with one card in a new column, shown at once while the daemon writes the file.
export function moved(b: TaskBoard, file: string, status: string): TaskBoard {
  return { ...b, lists: b.lists.map((l) => ({ ...l, tasks: l.tasks.map((t) => (t.file === file ? { ...t, status } : t)) })) };
}

// blocked reports whether a todo task still waits on a blocker that is not done or dropped.
export function blocked(l: TaskList, t: TaskCard): boolean {
  if (t.status !== "todo") return false;
  const closed = new Set(l.tasks.filter((x) => x.status === "done" || x.status === "dropped").map((x) => Number(x.nn)));
  return t.blockers.some((n) => !closed.has(Number(n)));
}

// rel is a task file's path from its project folder, how the card names its file.
export const rel = (file: string, project: string) => (project && file.startsWith(project + "/") ? file.slice(project.length + 1) : file);

// next is the column one step left (-1) or right (+1) of status, or "" past the edge.
export function next(status: string, step: number): string {
  const i = COLUMNS.findIndex((c) => c.id === status);
  return COLUMNS[i + step]?.id ?? "";
}
