import { board } from "../model";
import { useLive } from "../live";
import { G } from "../ui";
import "./chat.css";

const TITLE = { running: "Running", waiting: "Waiting on you", done: "Done", failed: "Failed" };

export default function Board() {
  const l = useLive();
  return (
    <section aria-label="Board">
      <h1 class="h">Board <span class="dim">{l.sessions.length} sessions</span></h1>
      <div class="cols">
        {board(l.sessions).map((c) => (
          <div class="col" role="group" aria-label={TITLE[c.state]}>
            <h2 class="h2">
              <G s={c.state} /> {TITLE[c.state]} <span class="dim">{c.items.length}</span>
            </h2>
            <ul class="list">
              {c.items.map((s) => (
                <li>
                  <a href={"#/sessions?s=" + encodeURIComponent(s.id)}>
                    <span class="strong id">{s.id}</span> <span class="dim">{s.kind}</span>
                  </a>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    </section>
  );
}
