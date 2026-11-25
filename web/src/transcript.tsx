import { useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";
import type { Row } from "./model";
import { G } from "./ui";
import { Heights } from "./virt";

const EST = 24;

// Transcript renders only the rows in view (plus overscan) and measures them after paint, so
// 10k messages scroll as smoothly as 10. It sticks to the bottom until you scroll up.
export function Transcript({ rows, toggle, verbose }: { rows: Row[]; toggle: (i: number) => void; verbose: boolean }) {
  const box = useRef<HTMLDivElement>(null);
  const inner = useRef<HTMLDivElement>(null);
  const h = useMemo(() => new Heights(EST), []);
  const keys = useRef<string[]>([]);
  const stick = useRef(true);
  const [, tick] = useState(0);
  const [view, setView] = useState({ top: 0, height: 600 });

  if (keys.current.length && rows.length && keys.current[0] !== rows[0].key) h.resize(0);
  h.resize(rows.length);
  keys.current = rows.map((r) => r.key);

  const w = h.window(view.top, view.height, 8);
  const total = h.total();

  useLayoutEffect(() => {
    let changed = false;
    inner.current?.querySelectorAll<HTMLElement>("[data-i]").forEach((el) => {
      changed = h.set(+el.dataset.i!, el.offsetHeight) || changed;
    });
    const b = box.current!;
    if (stick.current) b.scrollTop = b.scrollHeight;
    if (changed) tick((n) => n + 1);
  });

  useLayoutEffect(() => {
    const b = box.current!;
    const ro = new ResizeObserver(() => setView({ top: b.scrollTop, height: b.clientHeight }));
    ro.observe(b);
    return () => ro.disconnect();
  }, []);

  const onScroll = () => {
    const b = box.current!;
    stick.current = b.scrollHeight - b.scrollTop - b.clientHeight < 4;
    setView({ top: b.scrollTop, height: b.clientHeight });
  };

  const onKey = (e: KeyboardEvent) => {
    const b = box.current!;
    if (e.key === "End") (stick.current = true), (b.scrollTop = b.scrollHeight);
    else if (e.key === "Home") (stick.current = false), (b.scrollTop = 0);
    else return;
    e.preventDefault();
  };

  return (
    <div class="tx" ref={box} onScroll={onScroll} onKeyDown={onKey} tabIndex={0} role="log" aria-live="polite" aria-label="Transcript">
      {!rows.length && <p class="dim pad">No events yet. They stream in as the daemon works.</p>}
      <div style={{ height: total + "px", position: "relative" }}>
        <div ref={inner} style={{ transform: `translateY(${w.top}px)` }}>
          {rows.slice(w.from, w.to).map((r, k) => (
            <div key={r.key} data-i={w.from + k}>
              <RowView r={r} toggle={toggle} verbose={verbose} />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function RowView({ r, toggle, verbose }: { r: Row; toggle: (i: number) => void; verbose: boolean }) {
  if (r.type === "summary")
    return (
      <button class="blk sum" onClick={() => toggle(r.first)} aria-expanded="false">
        <G s={r.status} /> <span class="muted">{r.names.join(" · ")}</span>
      </button>
    );
  if (r.type === "tool") {
    const t = r.msg.tool!;
    const head = (
      <>
        <G s={t.status} /> <span class="strong">{t.name}</span> <span class="muted">{t.arg}</span>
        <span class="right dim">
          {t.stat} {t.took}
        </span>
      </>
    );
    return (
      <div class={"blk tool" + (r.open ? " open" : "")}>
        {verbose ? <div class="th">{head}</div> : <button class="th" onClick={() => toggle(r.msg.i)} aria-expanded={r.open}>{head}</button>}
        {r.open && !!t.lines?.length && <pre class="out">{t.lines.join("\n")}</pre>}
      </div>
    );
  }
  const m = r.msg;
  if (m.who === "you")
    return (
      <p class="you">
        <span class="ember">›</span> {m.text}
      </p>
    );
  return (
    <p class={"line " + m.kind}>
      <span class="at dim">{m.at}</span>
      <span>{m.text}</span>
    </p>
  );
}
