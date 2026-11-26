import { GlyphSparkline } from "@zovaris/sephiro/glyph-chart";
import "@zovaris/sephiro/css/glyph-chart.css";
import { href } from "./router";
import { count, stats, usd } from "./stats";
import { agentName, kindOf, open, sessions, type Sess, type Store } from "./store";
import { split, St } from "./ui";

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
