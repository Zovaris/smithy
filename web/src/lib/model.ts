// The daemon's event stream folded into what the screens show, the same way `smithy chat` does
// (internal/tui/chat_ui.go): a transcript, the sessions seen and the decisions waiting on you.

export type Ev = { seq: number; time: string; kind: string; session?: string; data: unknown };
export type Decision = { id: string; question: string; options?: string[]; asked?: string };
export type Tool = { name: string; arg?: string; stat?: string; took?: string; status?: string; lines?: string[] };
export type Msg = { i: number; who: "daemon" | "you"; kind: "text" | "tool" | "thinking" | "turn"; at: string; session: string; text: string; tool?: Tool };
export type SState = "running" | "waiting" | "done" | "failed";
export type Session = { id: string; kind: string; state: SState; at: string };
export type Model = { last: number; msgs: Msg[]; sessions: Session[]; pending: Decision[] };
export type Mode = "minimal" | "expandable" | "verbose";
export const MODES: Mode[] = ["minimal", "expandable", "verbose"];

export const empty = (): Model => ({ last: 0, msgs: [], sessions: [], pending: [] });

export function parseSSE(buf: string): { frames: { id: string; data: string }[]; rest: string } {
  const parts = buf.replace(/\r\n?/g, "\n").split("\n\n");
  const rest = parts.pop() ?? "";
  const frames: { id: string; data: string }[] = [];
  for (const p of parts) {
    let id = "";
    const data: string[] = [];
    for (const l of p.split("\n")) {
      if (l.startsWith("id:")) id = l.slice(3).trim();
      else if (l.startsWith("data:")) data.push(l.slice(5).replace(/^ /, ""));
    }
    if (data.length) frames.push({ id, data: data.join("\n") });
  }
  return { frames, rest };
}

export function sessionState(kind: string): SState {
  if (kind === "failed" || kind === "error") return "failed";
  if (["done", "finished", "merged", "turn", "stopped"].includes(kind)) return "done";
  if (kind === "decision_asked") return "waiting";
  return "running";
}

const hhmm = (t: string) => {
  const d = new Date(t);
  return isNaN(+d) ? "" : d.toTimeString().slice(0, 5);
};
const obj = (d: unknown): Record<string, unknown> => (d && typeof d === "object" ? (d as Record<string, unknown>) : {});
const str = (v: unknown) => (typeof v === "string" ? v : "");

function text(kind: string, session: string, d: Record<string, unknown>): string {
  if (kind === "decision_asked") return str(d.id) ? "? " + str(d.question) : "";
  if (kind === "decision_answered") return "answered: " + str(d.answer) + (str(d.by) ? " by " + str(d.by) : "");
  if (kind === "log") return str(d.line);
  return session ? kind + " " + session : kind;
}

export function apply(m: Model, e: Ev): Model {
  if (e.seq <= m.last) return m;
  const s = e.session ?? "", d = obj(e.data), at = hhmm(e.time);
  let { sessions, pending } = m;
  if (s) {
    const st: Session = { id: s, kind: e.kind, state: sessionState(e.kind), at };
    const i = sessions.findIndex((x) => x.id === s);
    sessions = i < 0 ? [...sessions, st] : sessions.map((x, j) => (j === i ? st : x));
  }
  if (e.kind === "decision_asked" && str(d.id) && !pending.some((p) => p.id === d.id)) pending = [...pending, d as unknown as Decision];
  if (e.kind === "decision_answered") pending = pending.filter((p) => p.id !== d.id);
  const base = { i: m.msgs.length, who: "daemon" as const, at, session: s };
  let msg: Msg | null = null;
  if (e.kind === "tool") msg = str(d.name) ? { ...base, kind: "tool", text: "", tool: d as unknown as Tool } : null;
  else if (e.kind === "thinking" || e.kind === "turn") msg = str(d.text) ? { ...base, kind: e.kind, text: str(d.text) } : null;
  else {
    const t = text(e.kind, s, d);
    msg = t ? { ...base, kind: "text", text: t } : null;
  }
  return { last: e.seq, sessions, pending, msgs: msg ? [...m.msgs, msg] : m.msgs };
}

export const say = (m: Model, t: string): Model => ({ ...m, msgs: [...m.msgs, { i: m.msgs.length, who: "you", kind: "text", at: hhmm(new Date().toISOString()), session: "", text: t }] });

export type Row =
  | { type: "text"; key: string; msg: Msg }
  | { type: "tool"; key: string; msg: Msg; open: boolean }
  | { type: "summary"; key: string; first: number; names: string[]; status: string };

const worse = (a: string, b?: string) => (b === "failed" || (b === "running" && a === "ok") ? b : a);

// rows is the transcript in a mode: minimal folds each run of tools into one summary line,
// expandable shows a header per tool (open when toggled), verbose opens all and adds thinking.
export function rows(msgs: Msg[], mode: Mode, open: Set<number>, session: string): Row[] {
  const out: Row[] = [];
  const list = session ? msgs.filter((m) => m.session === session) : msgs;
  for (let k = 0; k < list.length; k++) {
    const m = list[k];
    if ((m.kind === "thinking" || m.kind === "turn") && mode !== "verbose") continue;
    if (m.kind !== "tool") {
      out.push({ type: "text", key: "m" + m.i, msg: m });
      continue;
    }
    if (mode === "minimal" && !open.has(m.i)) {
      let status = "ok";
      const names: string[] = [];
      let j = k;
      while (j < list.length && list[j].kind === "tool") {
        names.push(list[j].tool!.name);
        status = worse(status, list[j].tool!.status);
        j++;
      }
      out.push({ type: "summary", key: "s" + m.i, first: m.i, names, status });
      k = j - 1;
      continue;
    }
    out.push({ type: "tool", key: "t" + m.i, msg: m, open: mode === "verbose" || open.has(m.i) });
  }
  return out;
}

export const COLUMNS: SState[] = ["running", "waiting", "done", "failed"];
export const board = (ss: Session[]) => COLUMNS.map((state) => ({ state, items: ss.filter((s) => s.state === state) }));

export const GLYPH: Record<string, string> = { running: "●", waiting: "?", done: "✓", ok: "✓", failed: "✗" };
export const glyph = (s?: string) => GLYPH[s ?? "ok"] ?? "✓";
