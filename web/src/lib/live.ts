// One live connection per tab: /v1/events streams everything from seq 0 (so the conversations
// rebuild on load) and resumes after the last seq on reconnect; /v1/state seeds the decisions.
// The stream is read with fetch (EventSource cannot send the bearer token). Screens subscribe with
// useLive; the actions below call the sessions and decisions API.
import { useEffect, useState } from "preact/hooks";
import { api } from "./auth";
import { apply, empty, parseSSE, type Ev, type Model } from "./model";
import { blank, fold, seed, type Sess, type Store } from "./store";
import { setEffortRules } from "./parity";
import type { Setup } from "./setup";

export type Live = { store: Store; auto: Model; conn: "connecting" | "connected" | "reconnecting"; error: string; setup: Setup | null; host: string };

let cur: Live = { store: blank(), auto: empty(), conn: "connecting", error: "", setup: null, host: "" };
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
  const auto = evs.filter((e) => !e.kind.startsWith("session_")).reduce(apply, cur.auto);
  set({ ...cur, store: evs.reduce(fold, cur.store), auto });
}

function push(e: Ev) {
  queued.push(e);
  if (!frame) frame = typeof requestAnimationFrame === "function" ? requestAnimationFrame(flush) : (setTimeout(flush, 16) as unknown as number);
}

async function stream() {
  for (let wait = 500; ; wait = Math.min(wait * 2, 10000)) {
    try {
      const r = await api("/v1/events", { headers: { Accept: "text/event-stream", "Last-Event-ID": String(cur.store.last) } });
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

export async function loadSetup(): Promise<Setup | null> {
  const s = await api("/v1/setup").then((r) => (r.ok ? r.json() : null)).catch(() => null);
  if (s) {
    setEffortRules(s.effort);
    set({ ...cur, setup: s });
  }
  return s;
}

export function start() {
  if (started) return;
  started = true;
  api("/v1/state")
    .then((r) => (r.ok ? r.json() : null))
    .then((st: { pending?: { id: string; question: string; options?: string[]; asked?: string }[]; serve?: { host?: string } } | null) => {
      if (st?.pending) set({ ...cur, store: seed(cur.store, st.pending) });
    })
    .catch(() => {});
  loadSetup();
  stream();
}

export const live = () => cur;

export function useLive(): Live {
  const [l, s] = useState(cur);
  useEffect(() => {
    subs.add(s);
    s(cur);
    return () => void subs.delete(s);
  }, []);
  return l;
}

async function call(method: string, path: string, body: unknown): Promise<any> {
  const r = await api(path, { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || `HTTP ${r.status}`);
  return j;
}

export const startSession = (b: { agent: string; model: string; effort?: string; dir: string; message: string }): Promise<Sess> => call("POST", "/v1/sessions", b);
export const sendMessage = (id: string, text: string) => call("POST", `/v1/sessions/${encodeURIComponent(id)}/messages`, { text });
export const stopSession = (id: string) => call("POST", `/v1/sessions/${encodeURIComponent(id)}/stop`, {});

// decide answers a decision; the daemon keeps the first answer, so a 409 is not an error here.
export async function decide(id: string, answer: string): Promise<string> {
  const r = await api("/v1/decisions", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ id, answer }) });
  if (r.ok || r.status === 409) {
    set({ ...cur, store: { ...cur.store, decisions: cur.store.decisions.map((d) => (d.id === id && d.answer == null ? { ...d, answer, by: "you" } : d)) } });
    return r.ok ? "" : "already answered";
  }
  const b = await r.json().catch(() => ({}));
  return (b as { error?: string }).error ?? "HTTP " + r.status;
}
