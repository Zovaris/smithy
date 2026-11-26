import { useEffect, useState } from "preact/hooks";
import { SegmentedControl } from "@zovaris/sephiro/segmented-control";
import "@zovaris/sephiro/css/segmented-control.css";
import { api } from "../auth";
import { useLive } from "../live";
import { agentName } from "../store";
import { St } from "../ui";

type Usage = { cost_usd?: number };
type Run = { id: string; agent: string; model?: string; skills?: string; started: string; seconds: number; passed: number; total: number; pass_rate: number; usage: Usage };
type Task = { pass: boolean; seconds: number } | null;
type Cmp = { pass_rate_delta: number; seconds_delta: number; tokens_delta: number; cost_delta: number | null; regressed: string[]; fixed: string[]; rows: { id: string; kind: string; a: Task; b: Task }[] };

const cost = (u: Usage) => (u.cost_usd == null ? "?" : "$" + u.cost_usd.toFixed(2));
const mark = (t: Task) => (t == null ? "-" : t.pass ? "pass" : "FAIL");
const sign = (n: number, d = 1) => (n >= 0 ? "+" : "") + n.toFixed(d);

type Status = { running: boolean; agent?: string; model?: string; started?: string; error?: string; log?: string };

// Bench lists saved `smithy bench run` results, starts a new run on an agent, and compares two
// runs the way `smithy bench compare` does.
export default function Bench() {
  const l = useLive();
  const [runs, setRuns] = useState<Run[] | null>(null);
  const [a, setA] = useState("");
  const [b, setB] = useState("");
  const [cmp, setCmp] = useState<Cmp | null>(null);
  const [st, setSt] = useState<Status>({ running: false });
  const [agent, setAgent] = useState("");
  const [err, setErr] = useState("");
  const ready = (l.setup?.agents ?? []).filter((r) => r.ready && r.kind === "cli");
  const pick0 = agent || l.setup?.default.agent || ready[0]?.name || "";

  const load = () =>
    api("/v1/bench").then((r) => (r.ok ? r.json() : [])).then((rs: Run[]) => {
      setRuns(rs);
      if (rs.length > 1) {
        setA(rs[1].id);
        setB(rs[0].id);
      }
    });
  const poll = () => api("/v1/bench/status").then((r) => (r.ok ? r.json() : { running: false })).then(setSt).catch(() => {});
  useEffect(() => {
    load();
    poll();
  }, []);
  useEffect(() => {
    if (!st.running) return;
    const t = setInterval(() => poll().then(() => load()), 3000);
    return () => clearInterval(t);
  }, [st.running]);
  const run = async () => {
    setErr("");
    const r = await api("/v1/bench/run", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ agent: pick0 }) });
    if (!r.ok) setErr((await r.json().catch(() => ({}))).error || `HTTP ${r.status}`);
    poll();
  };
  const runner = (
    <div class="card pad bench-run">
      <div class="top2" style="flex-wrap:wrap;gap:12px">
        {ready.length > 1 ? (
          <SegmentedControl ariaLabel="Agent" size="sm" value={pick0} onValueChange={setAgent} options={ready.map((r) => ({ value: r.name, label: agentName(r.name) }))} />
        ) : (
          <span>{pick0 ? agentName(pick0) : "No CLI agent is ready. Set one up in Settings."}</span>
        )}
        <span class="sp" />
        {st.running ? <St s="working" word={`running on ${agentName(st.agent ?? "")}`} /> : <button class="btn sm primary" disabled={!pick0} onClick={run}>Run the bench</button>}
      </div>
      <p class="t3" style="font-size:12px">Runs every task in a fresh temp repo, one headless run each; takes a few minutes and spends tokens. Same as <code>smithy bench run --agent {pick0 || "<cli>"}</code>.</p>
      {(err || st.error) && <p class="err">{err || "Last run: " + st.error}</p>}
      {st.log && (st.running || st.error) && <pre class="out" style="max-height:160px;overflow:auto">{st.log.split("\n").slice(-8).join("\n")}</pre>}
    </div>
  );
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
      <header><h2>Bench</h2><p>{runs.length ? `${runs.length} saved runs` : "No runs yet. Start one below; its result shows here when it ends."}</p></header>
      {runner}
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
