import { useEffect, useState } from "preact/hooks";
import { api } from "../auth";

type Usage = { cost_usd?: number };
type Run = { id: string; agent: string; model?: string; skills?: string; started: string; seconds: number; passed: number; total: number; pass_rate: number; usage: Usage };
type Task = { pass: boolean; seconds: number } | null;
type Cmp = { pass_rate_delta: number; seconds_delta: number; tokens_delta: number; cost_delta: number | null; regressed: string[]; fixed: string[]; rows: { id: string; kind: string; a: Task; b: Task }[] };

const cost = (u: Usage) => (u.cost_usd == null ? "?" : "$" + u.cost_usd.toFixed(2));
const mark = (t: Task) => (t == null ? "-" : t.pass ? "pass" : "FAIL");
const sign = (n: number, d = 1) => (n >= 0 ? "+" : "") + n.toFixed(d);

// Bench lists saved `smithy bench run` results; pick two to see `smithy bench compare`.
export default function Bench() {
  const [runs, setRuns] = useState<Run[] | null>(null);
  const [a, setA] = useState("");
  const [b, setB] = useState("");
  const [cmp, setCmp] = useState<Cmp | null>(null);
  useEffect(() => {
    api("/v1/bench").then((r) => (r.ok ? r.json() : [])).then((rs: Run[]) => {
      setRuns(rs);
      if (rs.length > 1) (setA(rs[1].id), setB(rs[0].id));
    });
  }, []);
  useEffect(() => {
    if (!a || !b) return;
    api(`/v1/bench/compare?a=${encodeURIComponent(a)}&b=${encodeURIComponent(b)}`).then((r) => (r.ok ? r.json() : null)).then(setCmp);
  }, [a, b]);
  if (!runs) return <p class="dim">loading…</p>;
  const pick = (v: string, set: (s: string) => void) => (
    <select value={v} onChange={(e) => set((e.target as HTMLSelectElement).value)}>
      {runs.map((r) => <option value={r.id}>{r.id}</option>)}
    </select>
  );
  return (
    <section aria-label="Bench">
      <h1 class="h">Bench <span class="dim">{runs.length} runs</span></h1>
      {!runs.length && <p class="dim">no runs yet: <code>smithy bench run --agent claude</code></p>}
      <ul class="list">
        {runs.map((r) => (
          <li>
            <span class="strong id">{r.id}</span> {r.passed}/{r.total} ({Math.round(r.pass_rate * 100)}%) · {Math.round(r.seconds)}s · {cost(r.usage)}
            <span class="right dim">{[r.agent, r.model, r.skills && "skills " + r.skills].filter(Boolean).join(" ")}</span>
          </li>
        ))}
      </ul>
      {runs.length > 1 && (
        <>
          <h2 class="h2 muted">Compare</h2>
          <p>a {pick(a, setA)} b {pick(b, setB)}</p>
          {cmp && (
            <>
              <p>
                pass rate {sign(cmp.pass_rate_delta * 100)}pt · time {sign(cmp.seconds_delta)}s · tokens {sign(cmp.tokens_delta, 0)} · cost{" "}
                {cmp.cost_delta == null ? "unknown" : sign(cmp.cost_delta, 2) + "$"}
              </p>
              {!!cmp.regressed.length && <p>regressed: {cmp.regressed.join(", ")}</p>}
              {!!cmp.fixed.length && <p>fixed: {cmp.fixed.join(", ")}</p>}
              <table>
                <thead><tr><th>task</th><th>kind</th><th>a</th><th>b</th></tr></thead>
                <tbody>
                  {cmp.rows.map((r) => (
                    <tr><td>{r.id}</td><td class="dim">{r.kind}</td><td>{mark(r.a)}</td><td>{mark(r.b)}</td></tr>
                  ))}
                </tbody>
              </table>
            </>
          )}
        </>
      )}
    </section>
  );
}
