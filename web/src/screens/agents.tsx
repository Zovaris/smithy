import { useLive } from "../live";
import { G } from "../ui";
import "./chat.css";

// Agents mirrors the TUI's tab panel: every session with its state, then what waits on you.
export default function Agents() {
  const l = useLive();
  const running = l.sessions.filter((s) => s.state === "running").length;
  return (
    <section aria-label="Agents">
      <h1 class="h">Agents <span class="dim">{running} running · {l.sessions.length} sessions</span></h1>
      <ul class="list">
        {l.sessions.map((s) => (
          <li class={s.state === "running" ? "on" : ""}>
            <a href={"#/sessions?s=" + encodeURIComponent(s.id)}>
              <G s={s.state} /> <span class="strong id">{s.id}</span> <span class="dim">{s.kind}</span>
              <span class="right dim">{s.at}</span>
            </a>
          </li>
        ))}
      </ul>
      {!l.sessions.length && <p class="dim">no sessions yet</p>}
      {!!l.pending.length && (
        <>
          <h2 class="h2 muted">Waiting on you</h2>
          <ul class="list">
            {l.pending.map((d) => (
              <li>
                <a href="#/decisions">
                  <G s="waiting" /> <span class="strong id">{d.id}</span> {d.question}
                </a>
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  );
}
