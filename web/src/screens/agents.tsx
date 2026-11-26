import { useLive } from "../live";
import { href } from "../router";
import { agentName, ago, sessions } from "../store";
import { StatsCharts } from "../charts";
import { stats } from "../stats";
import { St } from "../ui";

// Agents is the forge's stats, every session at a glance with its agent, model and state, and the
// detected agents.
export default function Agents() {
  const l = useLive();
  const ss = sessions(l.store);
  const working = ss.filter((s) => s.status === "working").length;
  const ready = l.setup?.agents ?? [];
  return (
    <section class="page" aria-label="Agents">
      <header>
        <h2>Agents</h2>
        <p>{working} working · {ss.length} sessions</p>
      </header>
      <div>
        <div class="lbl">Stats<span class="sp" /><span class="mono">smithy stats</span></div>
        <StatsCharts s={stats(l.store)} />
      </div>
      <div>
        <div class="lbl">Sessions</div>
        <div class="card">
          {!ss.length && <p class="hint" style="padding:12px">No sessions yet. <a href="#/new">Start one</a>.</p>}
          {ss.map((s) => (
            <a class="row2" href={href({ name: "session", id: s.id })}>
              <div class="top2">
                <span style="font-weight:500">{agentName(s.agent)}</span>
                <span class="mono t3">{s.model}</span>
                <span class="sp" />
                <St s={s.status} />
              </div>
              <div class="top2 t3" style="font-size:12px">
                <span style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{s.title}</span>
                <span class="sp" />
                <span class="mono">{ago(s.updated)}</span>
              </div>
            </a>
          ))}
        </div>
      </div>
      {!!ready.length && (
        <div>
          <div class="lbl">Detected on this machine<span class="sp" /><a class="t3" href="#/setup">Settings</a></div>
          <div class="card">
            {ready.map((r) => (
              <div class="row2">
                <div class="top2">
                  <span style="font-weight:500">{agentName(r.name)}</span>
                  <span class="mono t3" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{r.ready ? r.detail : r.fix}</span>
                  <span class="sp" />
                  <St s={r.ready ? "ready" : "missing"} />
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
      {!!l.auto.sessions.length && (
        <div>
          <div class="lbl">Background runs</div>
          <div class="card">
            {l.auto.sessions.map((s) => (
              <div class="row2"><div class="top2"><span class="mono">{s.id}</span><span class="t3">{s.kind}</span><span class="sp" /><span class="mono t3">{s.at}</span></div></div>
            ))}
          </div>
        </div>
      )}
    </section>
  );
}
