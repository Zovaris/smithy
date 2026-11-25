import { useLive } from "../live";
import { href } from "../router";
import { agentName, sessions, STATUS_WORD, type Status } from "../store";
import { St } from "../ui";

const COLS: Status[] = ["working", "needs_you", "done", "failed"];

// Board is the sessions by state, the same columns as the TUI's board.
export default function Board() {
  const l = useLive();
  const ss = sessions(l.store);
  return (
    <section class="page" aria-label="Board" style="max-width:1200px">
      <header>
        <h2>Board</h2>
        <p>{ss.length} sessions</p>
      </header>
      <div class="cols">
        {COLS.map((c) => {
          const items = ss.filter((s) => s.status === c);
          return (
            <div class="card" role="group" aria-label={STATUS_WORD[c]}>
              <h3><St s={c} /><span class="sp" /><span class="mono t3">{items.length}</span></h3>
              {items.map((s) => (
                <a class="row2" href={href({ name: "session", id: s.id })}>
                  <span style="font-weight:500">{s.title}</span>
                  <span class="t3" style="font-size:12px">{agentName(s.agent)}{s.model ? " · " + s.model : ""}</span>
                </a>
              ))}
            </div>
          );
        })}
      </div>
    </section>
  );
}
