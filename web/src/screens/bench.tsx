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
      if (rs.length > 1) {
        setA(rs[1].id);
        setB(rs[0].id);
      }
    });
  }, []);
  useEffect(() => {
    if (!a || !b) return;
    api(`/v1/bench/compare?a=${encodeURIComponent(a)}&b=${encodeURIComponent(b)}`).then((r) => (r.ok ? r.json() : null)).then(setCmp);
  }, [a, b]);
  if (!runs) return <section class="page"><p class="t3">Reading the runs…</p></section>;
  const pick = (v: string, set: (s: string) => void) => (
    <select class="btn sm" value={v} onChange={(e) => set((e.target as HTMLSelectElement).value)}>
      {runs.map((r) => <option value={r.id}>{r.id}</option>)}
    </select>
  );
  return (
    <section class="page" aria-label="Bench">
      <header><h2>Bench</h2><p>{runs.length ? `${runs.length} saved runs` : <>No runs yet: <code>smithy bench run --agent claude</code></>}</p></header>
      {!!runs.length && (
        <div class="card">
          {runs.map((r) => (
            <div class="row2">
              <div class="top2"><span class="mono" style="font-weight:500">{r.id}</span><span class="sp" /><span class="mono">{r.passed}/{r.total} · {Math.round(r.pass_rate * 100)}%</span></div>
              <span class="t3" style="font-size:12px">{[r.agent, r.model, r.skills && "skills " + r.skills].filter(Boolean).join(" · ")} · {Math.round(r.seconds)}s · {cost(r.usage)}</span>
            </div>
          ))}
        </div>
      )}
      {runs.length > 1 && (
        <>
          <div class="lbl">Compare</div>
          <p class="btns" style="align-items:center">a {pick(a, setA)} b {pick(b, setB)}</p>
          {cmp && (
            <>
              <p>
                pass rate {sign(cmp.pass_rate_delta * 100)}pt · time {sign(cmp.seconds_delta)}s · tokens {sign(cmp.tokens_delta, 0)} · cost{" "}
                {cmp.cost_delta == null ? "unknown" : sign(cmp.cost_delta, 2) + "$"}
              </p>
              {!!cmp.regressed.length && <p>regressed: {cmp.regressed.join(", ")}</p>}
              {!!cmp.fixed.length && <p>fixed: {cmp.fixed.join(", ")}</p>}
              <div class="card scrollx"><table class="tbl">
                <thead><tr><th>task</th><th>kind</th><th>a</th><th>b</th></tr></thead>
                <tbody>
                  {cmp.rows.map((r) => (
                    <tr><td>{r.id}</td><td class="t3">{r.kind}</td><td>{mark(r.a)}</td><td>{mark(r.b)}</td></tr>
                  ))}
                </tbody>
              </table></div>
            </>
          )}
        </>
      )}
    </section>
  );
}
