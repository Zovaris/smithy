import { GlyphSparkline } from "@zovaris/sephiro/glyph-chart";
import "@zovaris/sephiro/css/glyph-chart.css";
import { href } from "../lib/router";
import { count, stats, usd } from "../lib/stats";
import { agentName, kindOf, open, sessions, type Sess, type Store } from "../lib/store";
import { split, St } from "./ui";
import { useEffect, useState } from "preact/hooks";
import { loadTend, tendRows, type TendList } from "../lib/tend";

// Tend is /tend beside every page: tend.daemon's mode and each session's or batch item's PR.
function Tend({ store }: { store: Store }) {
  const [l, set] = useState<TendList | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    const get = () => loadTend().then((x) => (set(x), setErr("")), (e) => setErr(String((e as Error).message ?? e)));
    get();
    const i = setInterval(get, 15000);
    return () => clearInterval(i);
  }, []);
  const rows = l ? tendRows(l) : [];
  return (
    <section aria-label="Tend" id="tend">
      <h2>Tend<span class="sp" /><span class="mono t3">tend.daemon {l?.mode ?? "…"}</span></h2>
      <div class="card">
        {err && <p class="hint err">{err}</p>}
        {l && !rows.length && <p class="hint">No warden batch or chat session has an open PR to tend. <code>/set tend.daemon off|watch|fix</code></p>}
        {rows.map((r) => {
          const s = r.session ? store.sessions[r.session] : undefined;
          return (
            <div class="row2">
              <div class="top2">
                <a class="tt1 mono" href={r.url} target="_blank" rel="noreferrer">{r.label}</a>
                <span class="t3">{s ? <a href={href({ name: "session", id: s.id })}>{s.title}</a> : r.from}</span>
                <span class="sp" />
                <span class={"st " + r.tone}>{r.status}</span>
              </div>
              <span class="t3" style="font-size:12px">{r.hint}</span>
            </div>
          );
        })}
      </div>
    </section>
  );
}

const live = (s: Sess) => s.status === "working" || s.status === "needs_you";

function Row({ s, here }: { s: Sess; here: boolean }) {
  return (
    <a class="row2" href={href({ name: "session", id: s.id })} aria-current={here ? "page" : undefined}>
      <div class="top2">
        <span class="tt1">{s.title}</span>
        <span class="sp" />
        <St s={s.status} />
      </div>
      <span class="t3 top2" style="font-size:12px">
        {agentName(s.agent)}{s.model ? " · " + s.model : ""}<span class="sp" />{s.usage?.cost_usd != null && <span class="mono">{usd(s.usage.cost_usd)}</span>}
      </span>
    </a>
  );
}

// AgentsPanel is the TUI's tab panel beside every page: the running agents of this project first,
// then elsewhere, what waits on you, and the cost so far.
export default function AgentsPanel({ store, project, cur, host }: { store: Store; project: string; cur: string; host: string }) {
  const running = sessions(store).filter(live);
  const mine = running.filter((s) => project && s.dir === project);
  const other = running.filter((s) => !mine.includes(s));
  const waiting = open(store);
  const st = stats(store);
  const pname = project.split("/").pop() || "this project";
  return (
    <>
      <section aria-label="Agents">
        <h2>Agents<span class="sp" /><span class="mono t3">{running.length} running · {host}</span></h2>
        <div class="card">
          {!running.length && <p class="hint">No agent is working right now. <a href="#/new">Start one</a>.</p>}
          {!!mine.length && <div class="sub">{pname}</div>}
          {mine.map((s) => <Row s={s} here={s.id === cur} />)}
          {!!other.length && !!mine.length && <div class="sub">Elsewhere</div>}
          {other.map((s) => <Row s={s} here={s.id === cur} />)}
        </div>
      </section>
      {!!waiting.length && (
        <section aria-label="Waiting on you">
          <h2>Waiting on you<span class="sp" /><span class="mono need-n">{waiting.length}</span></h2>
          <div class="card">
            {waiting.map((d) => {
              const q = split(d.question);
              const s = store.sessions[d.session];
              return (
                <a class="row2" href={s ? href({ name: "session", id: s.id }) : "#/decisions"}>
                  <span>{q.code ? <code>{q.code}</code> : q.title}</span>
                  <span class="t3" style="font-size:12px">{kindOf(d)}{s ? " · " + s.title : ""}</span>
                </a>
              );
            })}
          </div>
        </section>
      )}
      <Tend store={store} />
      <section aria-label="Cost">
        <h2>Cost<span class="sp" /><span class="mono">{usd(st.cost)}</span></h2>
        <div class="card pad">
          <div class="top2 t3" style="font-size:12px"><span>{count(st.input + st.output)} tokens</span><span class="sp" /><span>{st.tps == null ? "" : st.tps.toFixed(0) + " tok/s"}</span></div>
          <GlyphSparkline label="Runs per hour, last 24 h" values={st.hourly} />
        </div>
      </section>
    </>
  );
}
