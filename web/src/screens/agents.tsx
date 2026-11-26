import { useState } from "preact/hooks";
import { useLive } from "../lib/live";
import { groupAgents, projectOf } from "../lib/projects";
import { href } from "../lib/router";
import type { Ready } from "../lib/setup";
import { agentName, ago, sessions } from "../lib/store";
import { StatsCharts } from "../components/charts";
import { stats } from "../lib/stats";
import { St } from "../components/ui";

function Copy({ text }: { text: string }) {
  const [ok, setOk] = useState(false);
  const copy = () => navigator.clipboard?.writeText(text).then(() => (setOk(true), setTimeout(() => setOk(false), 1200)));
  return <button class="btn sm" onClick={copy} aria-label={"Copy " + text}>{ok ? "Copied" : "Copy"}</button>;
}

const Ready1 = ({ r }: { r: Ready }) => (
  <div class="row1">
    <b>{agentName(r.name)}</b>
    <span class="mono t3 path">{r.detail}</span>
    <span class="sp" />
    <St s="ready" />
  </div>
);

// Agents is the agent picker's data at a glance: sessions (working first), the CLIs and API agents
// that are set up, and the missing ones with the command that sets each up.
export default function Agents() {
  const l = useLive();
  const g = groupAgents(l.setup?.agents ?? [], sessions(l.store));
  const working = g.sessions.filter((s) => s.status === "working").length;
  return (
    <section class="page agents-page" aria-label="Agents">
      <header>
        <h2>Agents</h2>
        <p>{working} working · {g.sessions.length} sessions · {g.clis.length + g.apis.length} set up · {g.missing.length} missing</p>
      </header>

      <div>
        <div class="lbl">Stats<span class="sp" /><span class="mono">smithy stats</span></div>
        <StatsCharts s={stats(l.store)} />
      </div>
      <div>
        <div class="lbl">Sessions<span class="sp" /><a class="t3" href="#/new">New session</a></div>
        <div class="card">
          {!g.sessions.length && <p class="hint">No sessions yet. <a href="#/new">Start one</a>.</p>}
          {g.sessions.map((s) => (
            <a class="row1" href={href({ name: "session", id: s.id })}>
              <St s={s.status} />
              <span class="title">{s.title}</span>
              <span class="sp" />
              <span class="t3 small hide-xs">{projectOf({ projects: [] }, s.dir)} · {agentName(s.agent)}{s.model ? " · " + s.model : ""}</span>
              <span class="mono t3 small">{ago(s.updated)}</span>
            </a>
          ))}
        </div>
      </div>

      <div class="cols2">
        <div>
          <div class="lbl">CLIs set up<span class="sp" /><span class="mono">{g.clis.length}</span></div>
          <div class="card">
            {!g.clis.length && <p class="hint">None found on this machine.</p>}
            {g.clis.map((r) => <Ready1 r={r} />)}
          </div>
        </div>
        <div>
          <div class="lbl">API agents set up<span class="sp" /><span class="mono">{g.apis.length}</span></div>
          <div class="card">
            {!g.apis.length && <p class="hint">No API key saved. <a href="#/setup">Settings</a></p>}
            {g.apis.map((r) => <Ready1 r={r} />)}
          </div>
        </div>
      </div>

      {!!g.missing.length && (
        <div>
          <div class="lbl">Missing<span class="sp" /><span class="t3">run the command on this machine</span></div>
          <div class="card">
            {g.missing.map((r) => (
              <div class="row1 miss">
                <b>{agentName(r.name)}</b>
                <code class="mono path">{r.fix || r.detail}</code>
                <span class="sp" />
                {r.fix && !r.fix.startsWith("see ") && <Copy text={r.fix} />}
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
              <div class="row1"><span class="mono">{s.id}</span><span class="t3">{s.kind}</span><span class="sp" /><span class="mono t3 small">{s.at}</span></div>
            ))}
          </div>
        </div>
      )}
    </section>
  );
}
