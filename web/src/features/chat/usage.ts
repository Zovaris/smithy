// /usage: the daemon's token report (GET /v1/usage, what smithy stats reads from the events log)
// laid out as the TUI's Usage modal, loaded only when the command runs.
import type { Info } from "../../components/info";
import { tokenUsage } from "../../lib/actions";

type U = { input_tokens: number; output_tokens: number; cache_read_tokens: number; cache_write_tokens: number; cost_usd?: number };
type Tokens = { total: U; agents: Record<string, U> | null; models: Record<string, U> | null; runs: number };
export type Report = { session: Tokens; today: Tokens; week: Tokens; all: Tokens };

const kilo = (n: number) => (n >= 1e6 ? (n / 1e6).toFixed(1) + "M" : n >= 1e3 ? (n / 1e3).toFixed(1) + "k" : String(n));
const sum = (u: U) => u.input_tokens + u.output_tokens + u.cache_read_tokens + u.cache_write_tokens;

// line is "in 12.0k · out 3.4k · cache 56.0k · $1.25", the cost only when reported.
export const line = (u: U) =>
  `in ${kilo(u.input_tokens)} · out ${kilo(u.output_tokens)} · cache ${kilo(u.cache_read_tokens + u.cache_write_tokens)}` + (u.cost_usd != null ? ` · $${u.cost_usd.toFixed(2)}` : "");

const rows = (kind: string, m: Record<string, U> | null) =>
  Object.entries(m ?? {}).sort(([a, x], [b, y]) => sum(y) - sum(x) || a.localeCompare(b)).map(([k, u]) => kind + " " + k + "\t" + line(u));

// usageInfo lays the report out by period, each with its total, then per agent and per model.
export function usageInfo(r: Report): Info {
  const lines: string[] = [];
  const periods = [["THIS SESSION", r.session], ["TODAY", r.today], ["LAST 7 DAYS", r.week], ["ALL TIME", r.all]] as const;
  for (const [head, t] of periods) {
    if (!t.runs) continue;
    lines.push("#" + head, `total · ${t.runs} run${t.runs === 1 ? "" : "s"}\t${line(t.total)}`, ...rows("agent", t.agents), ...rows("model", t.models));
  }
  return { title: "Usage", lines: lines.length ? lines : ["no token use recorded yet: agents report it when a run or turn ends"] };
}

export const loadUsage = async (session?: string): Promise<Info> =>
  usageInfo(await tokenUsage<Report>(session ?? ""));
