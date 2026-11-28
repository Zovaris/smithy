// ShellBlock is a `!cmd` run in the chat, as the TUI draws it: the command, exit status and time on
// one row, the output under a rule, muted, stderr tinted, folded after FOLD lines until opened.
import { useState } from "preact/hooks";
import "./shellblock.css";

export type ShellLine = { text: string; err?: boolean };
export type Shell = { cmd: string; lines: ShellLine[]; code: number; error?: string; took_ms?: number };

export const FOLD = 20;

export const took = (ms = 0) => (ms < 1000 ? ms + "ms" : ms < 60000 ? (ms / 1000).toFixed(1) + "s" : Math.floor(ms / 60000) + "m" + Math.round((ms % 60000) / 1000) + "s");

export default function ShellBlock({ x }: { x: Shell }) {
  const [open, setOpen] = useState(false);
  const all = x.lines.length ? x.lines : [{ text: "(no output)" }];
  const folded = !open && all.length > FOLD + 2;
  const shown = folded ? all.slice(0, FOLD) : all;
  const status = x.error ? x.error : x.code ? "exit " + x.code : "ok";
  return (
    <div class="shell" role="group" aria-label={"Shell: " + x.cmd}>
      <header>
        <span class="p">$</span>
        <span class="c" title={x.cmd}>{x.cmd}</span>
        <span class="m">{took(x.took_ms)}</span>
        <span class={"m " + (x.error || x.code ? "fail" : "ok")}>{status}</span>
      </header>
      <pre class="out">{shown.map((l, i) => <span key={i} class={l.err ? "e" : undefined}>{l.text + "\n"}</span>)}</pre>
      {all.length > FOLD + 2 && (
        <button type="button" class="more" aria-expanded={open} onClick={() => setOpen(!open)}>
          {open ? "fold the output" : `… ${all.length - FOLD} more lines`}
        </button>
      )}
    </div>
  );
}
