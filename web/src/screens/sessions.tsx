import { useMemo, useRef, useState } from "preact/hooks";
import { MODES, rows } from "../model";
import { decide, send, setMode, useLive } from "../live";
import { Transcript } from "../transcript";
import { Answer, G, choices } from "../ui";
import "./chat.css";

// Sessions is the chat: the sessions seen on the left, the transcript in one of the TUI's three
// modes, the oldest decision pinned on top and the input pinned at the bottom.
export default function Sessions() {
  const l = useLive();
  const [sel, setSel] = useState(() => new URLSearchParams(location.hash.split("?")[1]).get("s") ?? "");
  const [open, setOpen] = useState(() => new Set<number>());
  const [text, setText] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const rs = useMemo(() => rows(l.msgs, l.mode, open, sel), [l.msgs, l.mode, open, sel]);
  const toggle = (i: number) =>
    setOpen((o) => {
      const n = new Set(o);
      n.has(i) ? n.delete(i) : n.add(i);
      return n;
    });
  const running = l.sessions.filter((s) => s.state === "running");
  const top = l.pending[0];

  const onKey = (e: KeyboardEvent) => {
    if (e.key === "Enter") {
      const t = text.trim();
      if (t) send(t);
      setText("");
      return;
    }
    if (text || e.ctrlKey || e.metaKey || e.altKey) return;
    if (e.key === "v") {
      e.preventDefault();
      setMode(MODES[(MODES.indexOf(l.mode) + 1) % 3]);
    } else if (top) {
      const o = choices(top).find((c) => c[0] === e.key);
      if (o) {
        e.preventDefault();
        decide(top.id, o);
      }
    }
  };

  return (
    <div class="chat">
      <nav class="side" aria-label="Sessions">
        <button class="srow" aria-current={sel === "" ? "true" : undefined} onClick={() => setSel("")}>
          <span class="g dim">≡</span> all
        </button>
        {l.sessions.map((s) => (
          <button class="srow" aria-current={sel === s.id ? "true" : undefined} onClick={() => setSel(s.id)}>
            <G s={s.state} /> <span class="strong">{s.id}</span> <span class="dim">{s.kind}</span>
          </button>
        ))}
        {!l.sessions.length && <p class="dim pad">no sessions yet</p>}
      </nav>
      <section class="pane" aria-label="Chat">
        {top && <Answer d={top} compact />}
        {!!running.length && (
          <p class="run">
            <span class="ember">●</span> {running.length} running · {running.map((s) => s.id).join(" · ")}
          </p>
        )}
        <Transcript rows={rs} toggle={toggle} verbose={l.mode === "verbose"} />
        <div class="in">
          <span class="ember" aria-hidden="true">›</span>
          <input ref={input} value={text} onInput={(e) => setText(e.currentTarget.value)} onKeyDown={onKey} placeholder="Ask, or / for commands" aria-label="Message" />
        </div>
        <p class="foot dim">
          <span><kbd>enter</kbd> send</span>
          <button class="lnk" onClick={() => setMode(MODES[(MODES.indexOf(l.mode) + 1) % 3])}>
            <kbd>v</kbd> mode: {l.mode}
          </button>
          <span><kbd>ctrl+k</kbd> palette</span>
          <span class="right">{l.conn === "connected" ? "● live" : l.conn + (l.error ? " · " + l.error : "")}</span>
        </p>
      </section>
    </div>
  );
}
