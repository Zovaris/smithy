// One live connection per tab: /v1/state seeds the decisions, /v1/events streams the rest. The
// stream is read with fetch (EventSource cannot send the bearer token) and resumes after the last
// seq on reconnect. Screens subscribe with useLive.
import { useEffect, useState } from "preact/hooks";
import { api } from "./auth";
import { apply, empty, parseSSE, say, type Decision, type Ev, type Mode, type Model } from "./model";

export type Live = Model & { conn: "connecting" | "connected" | "reconnecting"; error: string; mode: Mode };

let cur: Live = { ...empty(), conn: "connecting", error: "", mode: "expandable" };
const subs = new Set<(l: Live) => void>();
let started = false;
let queued: Ev[] = [];
let frame = 0;

function set(l: Live) {
  cur = l;
  subs.forEach((f) => f(cur));
}

function flush() {
  frame = 0;
  const evs = queued;
  queued = [];
  set({ ...evs.reduce(apply, cur as Model), conn: cur.conn, error: cur.error, mode: cur.mode } as Live);
}

function push(e: Ev) {
  queued.push(e);
  if (!frame) frame = requestAnimationFrame(flush);
}

async function stream() {
  for (let wait = 500; ; wait = Math.min(wait * 2, 10000)) {
    try {
      const r = await api("/v1/events", { headers: { Accept: "text/event-stream", "Last-Event-ID": String(cur.last) } });
      if (!r.ok || !r.body) throw new Error("HTTP " + r.status);
      set({ ...cur, conn: "connected", error: "" });
      wait = 500;
      const rd = r.body.pipeThrough(new TextDecoderStream()).getReader();
      let buf = "";
      for (;;) {
        const { value, done } = await rd.read();
        if (done) break;
        const p = parseSSE(buf + value);
        buf = p.rest;
        for (const f of p.frames) {
          try {
            push(JSON.parse(f.data) as Ev);
          } catch {
            /* a frame that is not an event is skipped */
          }
        }
      }
      throw new Error("stream closed");
    } catch (e) {
      set({ ...cur, conn: "reconnecting", error: String((e as Error).message ?? e) });
      await new Promise((ok) => setTimeout(ok, wait));
    }
  }
}

export function start() {
  if (started) return;
  started = true;
  api("/v1/state")
    .then((r) => (r.ok ? r.json() : null))
    .then((st: { pending?: Decision[] } | null) => {
      if (st?.pending) set({ ...cur, pending: [...st.pending, ...cur.pending.filter((p) => !st.pending!.some((q) => q.id === p.id))] });
    })
    .catch(() => {});
  stream();
}

export const live = () => cur;
export const setMode = (mode: Mode) => set({ ...cur, mode });
export const send = (t: string) => set({ ...cur, ...say(cur, t) });

export async function decide(id: string, answer: string): Promise<string> {
  const r = await api("/v1/decisions", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ id, answer }) });
  if (r.ok || r.status === 409) {
    set({ ...cur, pending: cur.pending.filter((p) => p.id !== id) });
    return r.ok ? "" : "already answered";
  }
  const b = await r.json().catch(() => ({}));
  return (b as { error?: string }).error ?? "HTTP " + r.status;
}

export function useLive(): Live {
  const [l, s] = useState(cur);
  useEffect(() => {
    subs.add(s);
    s(cur);
    return () => void subs.delete(s);
  }, []);
  return l;
}
