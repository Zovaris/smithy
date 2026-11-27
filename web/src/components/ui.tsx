import { useState } from "preact/hooks";
import { decide } from "../lib/live";
import { kindOf, picks, primary, STATUS_CLASS, STATUS_WORD, type Dec, type Status } from "../lib/store";

export const St = ({ s, word }: { s: Status | "idle" | "ready" | "missing"; word?: string }) => {
  const cls = s === "idle" || s === "missing" ? "idle" : s === "ready" ? "done" : STATUS_CLASS[s];
  return <span class={"st " + cls}>{word ?? (s === "idle" || s === "ready" || s === "missing" ? s : STATUS_WORD[s])}</span>;
};

// command is the shell command or host a guard question names, shown as code under the title.
export function split(q: string): { title: string; code: string } {
  let m = /^Run this command\? ([\s\S]+)$/.exec(q);
  if (m) return { title: "Run a command that matches the guard?", code: m[1] };
  m = /^Fetch from (.+)\?$/.exec(q);
  if (m) return { title: "Fetch from a host the guard blocks?", code: m[1] };
  return { title: q, code: "" };
}

const LABEL: Record<string, string> = { "run once": "Run once", "always allow this pattern": "Always allow", deny: "Deny" };
export const label = (o: string) => LABEL[o] ?? o.charAt(0).toUpperCase() + o.slice(1);

// Decision is the question card: numbered options (a click or its number answers; on a
// multi-select question they toggle and Send answers), and a free-text "Other" when the asker takes one. The daemon keeps
// the first answer from any client, so a late click reads "already answered".
export function Decision({ d }: { d: Dec }) {
  const [busy, setBusy] = useState("");
  const [err, setErr] = useState("");
  const [picked, setPicked] = useState<Set<number>>(new Set());
  const [text, setText] = useState("");
  const { title, code } = split(d.question);
  if (d.answer != null)
    return (
      <div class="decide done" role="group" aria-label={d.question}>
        <div class="hd">
          <St s="idle" word="answered" />
          <span class="t3">{kindOf(d)}</span>
        </div>
        <span class="t2">
          {code ? <code>{code}</code> : d.question} · {label(d.answer)}
          {d.by ? <span class="t3"> · {d.by}</span> : null}
        </span>
      </div>
    );
  const go = async (a: string) => {
    if (!a.trim()) return;
    setBusy(a);
    setErr(await decide(d.id, a.trim()).catch((e) => String(e)));
    setBusy("");
  };
  const p = primary(d);
  const opts = d.multi ? d.options : [p, ...d.options.filter((o) => o !== p)];
  const toggle = (i: number) => setPicked((s) => {
      const n = new Set(s);
      if (!n.delete(i)) n.add(i);
      return n;
    });
  const key = (e: KeyboardEvent) => {
    if ((e.target as HTMLElement).tagName === "INPUT" || e.metaKey || e.ctrlKey || e.altKey) return;
    const i = Number(e.key) - 1;
    if (!Number.isInteger(i) || i < 0 || i >= opts.length) return;
    e.preventDefault();
    if (d.multi) toggle(i);
    else go(opts[i]);
  };
  return (
    <div class="decide" role="group" aria-label={d.question} data-decision={d.id} tabIndex={-1} onKeyDown={key}>
      <div class="hd">
        <St s="needs_you" word="waiting on you" />
        <span class="t3">{kindOf(d)}{d.multi ? " · pick any" : ""}</span>
      </div>
      <h3>{title}</h3>
      {code && <pre>{code}</pre>}
      <div class="btns">
        {opts.map((o, i) => (
          d.multi ? (
            <button class={"btn" + (picked.has(i) ? " on" : "")} aria-pressed={picked.has(i)} disabled={!!busy} onClick={() => toggle(i)}>
              <kbd>{i + 1}</kbd>{label(o)}
            </button>
          ) : (
            <button class={"btn" + (o === p ? " primary" : "")} disabled={!!busy} onClick={() => go(o)} data-primary={o === p ? "" : undefined}>
              <kbd>{i + 1}</kbd>{busy === o ? "…" : label(o)}
            </button>
          )
        ))}
        {d.multi && (
          <button class="btn primary" disabled={!!busy || !picked.size} onClick={() => go(picks(d.options, picked))}>
            Send<kbd>↵</kbd>
          </button>
        )}
      </div>
      {d.other && (
        <form class="other" onSubmit={(e) => { e.preventDefault(); go(text); }}>
          <input aria-label="Other answer" placeholder="Other: your own answer" value={text} onInput={(e) => setText((e.target as HTMLInputElement).value)} disabled={!!busy} />
          <button class="btn sm" disabled={!!busy || !text.trim()}>Answer</button>
        </form>
      )}
      {err && <p class="err" role="alert">{err}</p>}
    </div>
  );
}
