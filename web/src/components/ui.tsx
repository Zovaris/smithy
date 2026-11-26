import { useState } from "preact/hooks";
import { decide } from "../lib/live";
import { kindOf, primary, STATUS_CLASS, STATUS_WORD, type Dec, type Status } from "../lib/store";

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

// Decision is one question with a button per option; ↵ answers the primary one. The daemon keeps
// the first answer from any client, so a late click reads "already answered".
export function Decision({ d }: { d: Dec }) {
  const [busy, setBusy] = useState("");
  const [err, setErr] = useState("");
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
    setBusy(a);
    setErr(await decide(d.id, a).catch((e) => String(e)));
    setBusy("");
  };
  const p = primary(d);
  const opts = [p, ...d.options.filter((o) => o !== p)];
  return (
    <div class="decide" role="group" aria-label={d.question} data-decision={d.id}>
      <div class="hd">
        <St s="needs_you" />
        <span class="t3">{kindOf(d)}</span>
      </div>
      <h3>{title}</h3>
      {code && <pre>{code}</pre>}
      <div class="btns">
        {opts.map((o) => (
          <button class={"btn" + (o === p ? " primary" : "")} disabled={!!busy} onClick={() => go(o)} data-primary={o === p ? "" : undefined}>
            {busy === o ? "…" : label(o)}
            {o === p && <kbd>↵</kbd>}
          </button>
        ))}
      </div>
      {err && <p class="err" role="alert">{err}</p>}
    </div>
  );
}
