// The forge's numbers from the session events, the ones `smithy stats` shows: cost, tokens,
// tok/s and runs per hour over the last 24 h.
import type { Sess, Store } from "./store";
import { sessions } from "./store";

export type Stats = {
  runs: number;
  working: number;
  waiting: number;
  cost: number | null;
  input: number;
  output: number;
  tps: number | null;
  hourly: number[];
  costBy: { id: string; label: string; value: number }[];
  tokensBy: { id: string; label: string; value: number }[];
  tpsSeries: number[];
};

const HOUR = 3600_000;

// rate is a finished session's output tokens per second of wall time, or null when unknown.
export function rate(s: Sess): number | null {
  const out = s.usage?.output_tokens ?? 0;
  const secs = (Date.parse(s.updated) - Date.parse(s.created)) / 1000;
  return out > 0 && secs >= 1 ? out / secs : null;
}

export function stats(st: Store, now = Date.now()): Stats {
  const ss = sessions(st);
  const hourly = Array<number>(24).fill(0);
  for (const s of ss) {
    const age = now - Date.parse(s.created);
    if (age >= 0 && age < 24 * HOUR) hourly[23 - Math.floor(age / HOUR)]++;
  }
  const costs = ss.filter((s) => s.usage?.cost_usd != null);
  const rates = ss.map(rate).filter((r): r is number => r != null);
  const top = (f: (s: Sess) => number) =>
    ss.map((s) => ({ id: s.id, label: s.title, value: f(s) })).filter((x) => x.value > 0).sort((a, b) => b.value - a.value).slice(0, 6);
  return {
    runs: ss.length,
    working: ss.filter((s) => s.status === "working").length,
    waiting: st.decisions.filter((d) => d.answer == null).length,
    cost: costs.length ? costs.reduce((n, s) => n + (s.usage!.cost_usd ?? 0), 0) : null,
    input: ss.reduce((n, s) => n + (s.usage?.input_tokens ?? 0), 0),
    output: ss.reduce((n, s) => n + (s.usage?.output_tokens ?? 0), 0),
    tps: rates.length ? rates.reduce((a, b) => a + b, 0) / rates.length : null,
    hourly,
    costBy: top((s) => s.usage?.cost_usd ?? 0),
    tokensBy: top((s) => (s.usage?.input_tokens ?? 0) + (s.usage?.output_tokens ?? 0)),
    tpsSeries: [...ss].reverse().map(rate).filter((r): r is number => r != null).slice(-24),
  };
}

export const usd = (n: number | null) => (n == null ? "?" : "$" + n.toFixed(2));
export function count(n: number): string {
  if (n >= 1e6) return (n / 1e6).toFixed(1) + "M";
  if (n >= 1e3) return (n / 1e3).toFixed(1) + "k";
  return String(Math.round(n));
}
