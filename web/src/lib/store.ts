// The daemon's session events (session_*, decision_*) folded into what the web shows: each
// session with its status and usage, its conversation as turns, its latest diff, and every
// decision with the session it belongs to.
import type { Ev } from "./model";

export type Status = "working" | "needs_you" | "done" | "failed";
export type Usage = { input_tokens?: number; output_tokens?: number; cost_usd?: number };
export type Sess = {
  id: string; agent: string; model?: string; effort?: string; dir: string; title: string;
  status: Status; detail?: string; created: string; updated: string; usage?: Usage;
};
export type Tool = { id?: string; name: string; args?: unknown; output?: string; is_error?: boolean };
export type Part = { kind: "text"; text: string } | { kind: "tools"; tools: Tool[] } | { kind: "decision"; id: string };
export type Item = { kind: "you"; text: string; at: string } | { kind: "agent"; parts: Part[]; at: string };
export type Diff = { stat: string; patch: string; truncated: boolean; at: string };
export type Dec = { id: string; session: string; question: string; options: string[]; asked: string; answer?: string; by?: string };
export type Store = {
  last: number;
  order: string[];
  sessions: Record<string, Sess>;
  convo: Record<string, Item[]>;
  diffs: Record<string, Diff>;
  decisions: Dec[];
  log: string[];
};

export const blank = (): Store => ({ last: 0, order: [], sessions: {}, convo: {}, diffs: {}, decisions: [], log: [] });

const obj = (d: unknown): Record<string, any> => (d && typeof d === "object" ? (d as Record<string, any>) : {});
const str = (v: unknown) => (typeof v === "string" ? v : "");
// prose drops the machine lines a CLI agent's log can leak into its text (one JSON object per
// line, stream-json); people never write those, so the conversation keeps only what reads.
export function prose(t: string): string {
  return t
    .split("\n")
    .filter((l) => {
      const x = l.trim();
      if (!x.startsWith("{") || !x.endsWith("}")) return true;
      try {
        return typeof JSON.parse(x) !== "object";
      } catch {
        return true;
      }
    })
    .join("\n");
}

const STATUSES: Status[] = ["working", "needs_you", "done", "failed"];

// sessionOf is the session a decision belongs to: the event's, else perm:<session>/<n>.
export function sessionOf(id: string, session = ""): string {
  if (session) return session;
  const m = /^perm:([^/]+)\//.exec(id);
  return m ? m[1] : "";
}

// touch moves a session to the top of the list: the most recently updated first, like the API.
const touch = (s: Store, id: string) => [id, ...s.order.filter((x) => x !== id)];

function agentTurn(items: Item[], at: string): { items: Item[]; turn: Extract<Item, { kind: "agent" }> } {
  const last = items[items.length - 1];
  if (last?.kind === "agent") {
    const turn = { ...last, parts: [...last.parts] };
    return { items: [...items.slice(0, -1), turn], turn };
  }
  const turn: Extract<Item, { kind: "agent" }> = { kind: "agent", parts: [], at };
  return { items: [...items, turn], turn };
}

export function fold(s: Store, e: Ev): Store {
  if (e.seq <= s.last) return s;
  const d = obj(e.data), id = e.session ?? "", at = e.time;
  const next: Store = { ...s, last: e.seq };
  const sess = s.sessions[id];
  const convo = s.convo[id] ?? [];
  switch (e.kind) {
    case "session_started": {
      const sid = str(d.id) || id;
      if (!sid) return next;
      next.sessions = { ...s.sessions, [sid]: { ...(d as Sess), id: sid, status: STATUSES.includes(d.status) ? d.status : "done", created: at, updated: at } };
      next.order = [sid, ...s.order.filter((x) => x !== sid)];
      next.convo = { ...s.convo, [sid]: s.convo[sid] ?? [] };
      return next;
    }
    case "session_status": {
      if (!sess) return next;
      const st: Status = STATUSES.includes(d.status) ? d.status : sess.status;
      next.sessions = { ...s.sessions, [id]: { ...sess, status: st, detail: str(d.detail), updated: at } };
      next.order = touch(s, id);
      return next;
    }
    case "session_model": {
      if (!sess) return next;
      next.sessions = { ...s.sessions, [id]: { ...sess, model: str(d.model), effort: str(d.effort) } };
      return next;
    }
    case "session_usage": {
      if (!sess) return next;
      const u = sess.usage ?? {};
      const cost = d.cost_usd == null && u.cost_usd == null ? undefined : (u.cost_usd ?? 0) + (d.cost_usd ?? 0);
      next.sessions = { ...s.sessions, [id]: { ...sess, usage: { input_tokens: (u.input_tokens ?? 0) + (d.input_tokens ?? 0), output_tokens: (u.output_tokens ?? 0) + (d.output_tokens ?? 0), cost_usd: cost } } };
      return next;
    }
    case "session_message": {
      if (!sess) return next;
      const text = str(d.text);
      if (d.role === "user") next.convo = { ...s.convo, [id]: [...convo, { kind: "you", text, at }] };
      else {
        const { items, turn } = agentTurn(convo, at);
        // The reply streams as session_text first. When the final message says something else (a
        // CLI's log can stream noise), it replaces the streamed text and keeps the tools.
        const streamed = turn.parts.flatMap((p) => (p.kind === "text" ? [p.text] : [])).join("");
        if (text.trim() && streamed.trim() !== text.trim()) {
          turn.parts = turn.parts.filter((p) => p.kind !== "text");
          turn.parts.push({ kind: "text", text });
        }
        next.convo = { ...s.convo, [id]: items };
      }
      next.sessions = { ...s.sessions, [id]: { ...sess, updated: at } };
      next.order = touch(s, id);
      return next;
    }
    case "session_text": {
      const t = str(d.text);
      if (!sess || !t) return next;
      const { items, turn } = agentTurn(convo, at);
      const p = turn.parts[turn.parts.length - 1];
      if (p?.kind === "text") turn.parts[turn.parts.length - 1] = { kind: "text", text: p.text + t };
      else turn.parts.push({ kind: "text", text: t });
      next.convo = { ...s.convo, [id]: items };
      return next;
    }
    case "session_tool": {
      if (!sess || !str(d.name)) return next;
      const { items, turn } = agentTurn(convo, at);
      const p = turn.parts[turn.parts.length - 1];
      if (p?.kind === "tools") turn.parts[turn.parts.length - 1] = { kind: "tools", tools: [...p.tools, d as Tool] };
      else turn.parts.push({ kind: "tools", tools: [d as Tool] });
      next.convo = { ...s.convo, [id]: items };
      return next;
    }
    case "session_diff": {
      if (!sess) return next;
      next.diffs = { ...s.diffs, [id]: { stat: str(d.stat), patch: str(d.patch), truncated: !!d.truncated, at } };
      return next;
    }
    case "decision_asked": {
      const did = str(d.id);
      if (!did || s.decisions.some((x) => x.id === did)) return next;
      const sid = sessionOf(did, id);
      next.decisions = [...s.decisions, { id: did, session: sid, question: str(d.question), options: Array.isArray(d.options) && d.options.length ? d.options : ["yes", "no"], asked: str(d.asked) || at }];
      if (sid && s.sessions[sid]) {
        const { items, turn } = agentTurn(s.convo[sid] ?? [], at);
        turn.parts.push({ kind: "decision", id: did });
        next.convo = { ...s.convo, [sid]: items };
      }
      return next;
    }
    case "log": {
      if (str(d.line)) next.log = [...s.log, str(d.line)].slice(-200);
      return next;
    }
    case "decision_answered": {
      const did = str(d.id);
      next.decisions = s.decisions.map((x) => (x.id === did ? { ...x, answer: str(d.answer), by: str(d.by) } : x));
      return next;
    }
  }
  return next;
}

// seed adds the pending decisions /v1/state lists that the stream has not shown yet.
export function seed(s: Store, pending: { id: string; question: string; options?: string[]; asked?: string }[]): Store {
  const add = pending.filter((p) => !s.decisions.some((d) => d.id === p.id));
  if (!add.length) return s;
  return { ...s, decisions: [...s.decisions, ...add.map((p) => ({ id: p.id, session: sessionOf(p.id), question: p.question, options: p.options?.length ? p.options : ["yes", "no"], asked: p.asked ?? "" }))] };
}

export const open = (s: Store) => s.decisions.filter((d) => d.answer == null);
export const sessions = (s: Store) => s.order.map((id) => s.sessions[id]).filter(Boolean);

export const STATUS_WORD: Record<Status, string> = { working: "working", needs_you: "needs you", done: "done", failed: "failed" };
export const STATUS_CLASS: Record<Status, string> = { working: "run", needs_you: "need", done: "done", failed: "fail" };

// guardKind names what a decision is, for the small label over its card.
export function kindOf(d: Dec): string {
  if (d.id.startsWith("guard:")) return d.question.startsWith("Fetch from") ? "guard · blocked fetch" : "guard · blocked command";
  if (d.id.startsWith("perm:")) return "permission";
  return "decision";
}

// primary is the option the ↵ key answers: deny for the guard (the safe one), else the first.
export const primary = (d: Dec) => (d.options.includes("deny") ? "deny" : d.options[0]);

// agentName is how a session's agent reads in the UI.
export function agentName(agent: string): string {
  const a = agent.replace(/^acp:/, "");
  const NAMES: Record<string, string> = { claude: "Claude Code", codex: "Codex", opencode: "OpenCode", agy: "Antigravity", grok: "Grok", cursor: "Cursor", amp: "Amp", goose: "Goose", copilot: "Copilot", qwen: "Qwen" };
  if (a.startsWith("api:")) return a.slice(4).split("/")[0].replace(/^\w/, (c) => c.toUpperCase()) + " API";
  return NAMES[a] ?? (a || "agent");
}

export function initials(agent: string): string {
  const n = agentName(agent).replace(/ API$/, "").split(/\s+/);
  return (n.length > 1 ? n[0][0] + n[1][0] : n[0].slice(0, 2)).toUpperCase();
}

export function ago(iso: string, now = Date.now()): string {
  const t = Date.parse(iso);
  if (isNaN(t)) return "";
  const s = Math.max(0, Math.round((now - t) / 1000));
  if (s < 45) return "now";
  if (s < 3600) return Math.round(s / 60) + " min ago";
  if (s < 86400) return Math.round(s / 3600) + " h ago";
  return Math.round(s / 86400) + " d ago";
}

// argLine is the one-line argument a tool row shows: the path, command or pattern it got.
export function argLine(t: Tool): string {
  const a = obj(typeof t.args === "string" ? safe(t.args) : t.args);
  for (const k of ["file_path", "path", "command", "cmd", "pattern", "url", "query", "description"]) if (typeof a[k] === "string") return a[k];
  const v = Object.values(a).find((x) => typeof x === "string");
  return typeof v === "string" ? v : typeof t.args === "string" ? t.args : "";
}

function safe(s: string): unknown {
  try {
    return JSON.parse(s);
  } catch {
    return s;
  }
}
