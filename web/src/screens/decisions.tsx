import { useLive } from "../live";
import { href } from "../router";
import { open } from "../store";
import { Decision } from "../ui";

// Decisions lists every question waiting on you across sessions, then the ones answered lately.
export default function Decisions() {
  const l = useLive();
  const waiting = open(l.store);
  const done = l.store.decisions.filter((d) => d.answer != null).slice(-10).reverse();
  return (
    <section class="page" aria-label="Decisions">
      <header>
        <h2>Decisions</h2>
        <p>{waiting.length ? `${waiting.length} waiting on you. Any client can answer; the first answer counts.` : "Nothing waits on you."}</p>
      </header>
      {waiting.map((d) => (
        <div>
          {l.store.sessions[d.session] && <a class="lbl" href={href({ name: "session", id: d.session })}>{l.store.sessions[d.session].title}</a>}
          <Decision d={d} />
        </div>
      ))}
      {!!done.length && (
        <div>
          <div class="lbl">Answered</div>
          <div style="display:flex;flex-direction:column;gap:8px">{done.map((d) => <Decision d={d} />)}</div>
        </div>
      )}
    </section>
  );
}
