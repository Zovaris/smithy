// GET /v1/tend as the TUI's /tend panel reads it: the PRs the daemon keeps green, by session or batch.
import { api } from "./auth";

export type TendPR = { url: string; batch: string; item: string; session?: string; dir: string; action: string; checks: string; status: string; pid?: number; error?: string };
export type TendList = { mode: string; prs: TendPR[] };
export type TendRow = { from: string; session?: string; status: string; tone: "done" | "fail" | "run"; url: string; label: string; hint: string };

export async function loadTend(): Promise<TendList> {
  const r = await api("/v1/tend");
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return r.json();
}

// tendRows is each PR's line: where it comes from, its status and what tend does next, as tendPanel.
export function tendRows(l: TendList): TendRow[] {
  return (l.prs ?? []).map((p) => {
    let hint = p.action + " · checks " + p.checks;
    if (p.error) hint = p.error;
    else if (p.pid) hint += " · tend session pid " + p.pid;
    const tone = p.status === "green" || p.status === "closed" ? "done" : p.status === "error" || p.status === "needs-you" ? "fail" : "run";
    const n = /\/pull\/(\d+)$/.exec(p.url);
    return { from: p.batch ? p.batch + "/" + p.item : "session " + (p.session ?? ""), session: p.batch ? undefined : p.session, status: p.status, tone, url: p.url, label: n ? "#" + n[1] : p.url, hint };
  });
}
