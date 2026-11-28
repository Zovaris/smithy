// A session's linked work as GET /v1/sessions/{id}/links returns it (and session_links pushes
// it live): its PRs and its Linear and GitHub issues, in the same words as the TUI's /links.
import { api } from "./auth";

export type LinkPR = { url: string; number: number; title: string; state: string; draft?: boolean; mergeable?: string; review?: string; checks?: string; additions: number; deletions: number; head?: string };
export type LinkIssue = { kind: "linear" | "github"; id: string; title: string; url: string; status: string; assignee?: string; milestone?: string };
export type Links = { session: string; prs: LinkPR[]; issues: LinkIssue[]; error?: string; checked: string };
export type Work = { prs: (LinkPR & { sessions: string[] })[]; issues: (LinkIssue & { sessions: string[] })[] };

async function json<T>(path: string): Promise<T> {
  const r = await api(path);
  if (!r.ok) throw new Error((await r.text()).trim() || `HTTP ${r.status}`);
  return r.json();
}

export const loadLinks = (id: string) => json<Links>("/v1/sessions/" + encodeURIComponent(id) + "/links");
export const loadWork = (dir: string) => json<Work>("/v1/links" + (dir ? "?dir=" + encodeURIComponent(dir) : ""));

const lines = (p: LinkPR) => `+${p.additions} −${p.deletions}`;

export function prHint(p: LinkPR): string {
  const parts = [p.state, lines(p)];
  if (p.draft) parts.push("draft");
  if (p.checks) parts.push("checks " + p.checks);
  if (p.review) parts.push(p.review.toLowerCase().replaceAll("_", " "));
  if (p.mergeable) parts.push(p.mergeable.toLowerCase());
  return parts.join(" · ");
}

export const issueHint = (i: LinkIssue) => [i.status, i.assignee, i.milestone].filter(Boolean).join(" · ");

export const prTone = (p: LinkPR) => (p.state === "MERGED" || p.checks === "green" ? "done" : p.checks === "red" || p.state === "CLOSED" ? "fail" : "run");

export function linkSummary(l: Links | undefined): string {
  if (!l) return "";
  const prs = (l.prs ?? []).map((p) => `PR #${p.number} ${p.checks || (p.state !== "OPEN" ? p.state.toLowerCase() : "")} ${lines(p)}`.replace(/  +/g, " "));
  const issues = (l.issues ?? []).map((i) => `${i.id} ${i.status}`);
  return [...prs, ...issues].join(" · ");
}
