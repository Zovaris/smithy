// Text-glyph charts, the TUI's look on the web: bars drawn in eighth blocks, a block sparkline.
import { count, type Stats, usd } from "./stats";

const EIGHTHS = " ▏▎▍▌▋▊▉";
const SPARK = "▁▂▃▄▅▆▇█";

export function bar(value: number, max: number, width = 20): string {
  if (max <= 0 || value <= 0) return "";
  const n = Math.max(1, Math.round((Math.min(value, max) / max) * width * 8));
  return "█".repeat(Math.floor(n / 8)) + (n % 8 ? EIGHTHS[n % 8] : "");
}

export function spark(values: number[]): string {
  const max = Math.max(0, ...values);
  return values.map((v) => (max <= 0 || v <= 0 ? " " : SPARK[Math.min(7, Math.floor((v / max) * 7.999))])).join("");
}

function Bars({ label, rows, fmt }: { label: string; rows: { id: string; label: string; value: number }[]; fmt: (n: number) => string }) {
  const max = Math.max(0, ...rows.map((r) => r.value));
  return (
    <figure class="gc" aria-label={label}>
      <figcaption>{label}</figcaption>
      {!rows.length && <p class="t3">No data yet.</p>}
      {rows.map((r) => (
        <div class="gb" title={r.label}>
          <span class="gl">{r.label}</span>
          <span class="gg" aria-hidden="true">{bar(r.value, max, 16)}</span>
          <span class="gv mono">{fmt(r.value)}</span>
        </div>
      ))}
    </figure>
  );
}

function Spark({ label, values, value }: { label: string; values: number[]; value: string }) {
  return (
    <figure class="gc" aria-label={`${label}: ${value}`}>
      <figcaption>{label}<span class="sp" /><span class="mono">{value}</span></figcaption>
      <span class="gs" aria-hidden="true">{values.length ? spark(values) : "·"}</span>
    </figure>
  );
}

// StatsCharts is `smithy stats` for the web: cost, tokens, tok/s and runs per hour.
export function StatsCharts({ s }: { s: Stats }) {
  return (
    <div class="charts">
      <Spark label="Runs per hour, last 24 h" values={s.hourly} value={String(s.hourly.reduce((a, b) => a + b, 0))} />
      <Spark label="tok/s by run" values={s.tpsSeries} value={s.tps == null ? "?" : s.tps.toFixed(0) + " avg"} />
      <Bars label={`Cost · ${usd(s.cost)}`} rows={s.costBy} fmt={(n) => usd(n)} />
      <Bars label={`Tokens · ${count(s.input)} in · ${count(s.output)} out`} rows={s.tokensBy} fmt={count} />
    </div>
  );
}
