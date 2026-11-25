import { useLive } from "../live";
import { Answer } from "../ui";
import "./chat.css";

export default function Decisions() {
  const l = useLive();
  return (
    <section aria-label="Decisions waiting on you">
      <h1 class="h">Waiting on you <span class="dim">{l.pending.length}</span></h1>
      {l.pending.map((d) => (
        <Answer d={d} />
      ))}
      {!l.pending.length && <p class="dim"><span class="g ok">✓</span> Nothing waits on you.</p>}
    </section>
  );
}
