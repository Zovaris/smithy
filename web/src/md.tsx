import type { ComponentChildren } from "preact";

// Md renders the small markdown agents write (paragraphs, - and 1. lists, ``` fences, **bold**,
// `code`) as elements, never as HTML, so agent text cannot inject markup.
function inline(t: string): ComponentChildren[] {
  const out: ComponentChildren[] = [];
  const re = /`([^`]+)`|\*\*([^*]+)\*\*/g;
  let last = 0, m: RegExpExecArray | null;
  while ((m = re.exec(t))) {
    if (m.index > last) out.push(t.slice(last, m.index));
    out.push(m[1] != null ? <code>{m[1]}</code> : <strong>{m[2]}</strong>);
    last = m.index + m[0].length;
  }
  if (last < t.length) out.push(t.slice(last));
  return out;
}

export type Block = { t: "p" | "ul" | "ol" | "pre"; lines: string[] };

export function blocks(src: string): Block[] {
  const out: Block[] = [];
  const ls = src.replace(/\r/g, "").split("\n");
  for (let i = 0; i < ls.length; i++) {
    const l = ls[i];
    if (l.startsWith("```")) {
      const body: string[] = [];
      for (i++; i < ls.length && !ls[i].startsWith("```"); i++) body.push(ls[i]);
      out.push({ t: "pre", lines: body });
      continue;
    }
    if (!l.trim()) continue;
    const ul = /^\s*[-*] (.*)$/.exec(l), ol = /^\s*\d+[.)] (.*)$/.exec(l);
    const t = ul ? "ul" : ol ? "ol" : "p";
    const text = ul ? ul[1] : ol ? ol[1] : l;
    const prev = out[out.length - 1];
    if (prev && prev.t === t && (t !== "p" || ls[i - 1]?.trim())) prev.lines.push(text);
    else out.push({ t, lines: [text] });
  }
  return out;
}

export function Md({ text }: { text: string }) {
  return (
    <div class="md">
      {blocks(text).map((b) =>
        b.t === "pre" ? <pre>{b.lines.join("\n")}</pre>
        : b.t === "p" ? <p>{b.lines.flatMap((l, i) => (i ? [<br />, ...inline(l)] : inline(l)))}</p>
        : b.t === "ul" ? <ul>{b.lines.map((l) => <li>{inline(l)}</li>)}</ul>
        : <ol>{b.lines.map((l) => <li>{inline(l)}</li>)}</ol>,
      )}
    </div>
  );
}
