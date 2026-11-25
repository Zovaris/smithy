import { useMemo, useState } from "preact/hooks";
import { parse, short, totals, type Comment, type File } from "./diff";
import { I } from "./icons";
import { href } from "./router";
import { ago, agentName, type Dec, type Diff, type Sess } from "./store";
import { label, St, split } from "./ui";

function Note({ c, onDrop }: { c: Comment; onDrop: () => void }) {
  return (
    <div class="cm">
      {c.text}
      <br />
      <span class="t3">You · sent with the next message · </span>
      <button class="t3" onClick={onDrop} style="text-decoration:underline">remove</button>
    </div>
  );
}

function FileDiff({ f, comments, onComment, onDrop }: { f: File; comments: Comment[]; onComment: (c: Comment) => void; onDrop: (c: Comment) => void }) {
  const [at, setAt] = useState(0);
  const [text, setText] = useState("");
  const save = () => {
    if (text.trim()) onComment({ path: f.path, line: at, text: text.trim() });
    setAt(0);
    setText("");
  };
  return (
    <div class="diff">
      {f.lines.map((l) => {
        const mine = comments.filter((c) => c.path === f.path && c.line === l.n && l.t !== "del");
        const cls = { hunk: "h", add: "ad", del: "dl", ctx: "" }[l.t];
        return (
          <>
            {l.t === "hunk" || l.t === "del" ? (
              <div class={"l " + cls}><span class="ln">{l.t === "hunk" ? "…" : l.n}</span><span class="tx">{l.text}</span></div>
            ) : (
              <button class={"l " + cls} onClick={() => setAt(l.n!)} aria-label={`Comment on line ${l.n}`} title="Comment on this line">
                <span class="ln">{l.n}</span><span class="tx">{l.text}</span>
              </button>
            )}
            {mine.map((c) => <Note c={c} onDrop={() => onDrop(c)} />)}
            {at !== 0 && at === l.n && l.t !== "del" && (
              <div class="cm">
                <textarea value={text} onInput={(e) => setText(e.currentTarget.value)} placeholder={`Comment on line ${at}`} aria-label={`Comment on ${f.path} line ${at}`} ref={(t) => t?.focus()}
                  onKeyDown={(e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) save(); if (e.key === "Escape") setAt(0); }} />
                <div class="btns">
                  <button class="btn sm primary" onClick={save} disabled={!text.trim()}>Add comment</button>
                  <button class="btn sm" onClick={() => setAt(0)}>Cancel</button>
                </div>
              </div>
            )}
          </>
        );
      })}
    </div>
  );
}

// Changes is the session's working-tree diff after its last turn. Click a line to comment; the
// comments go in front of your next message.
export function Changes({ diff, comments, onComment, onDrop }: { diff?: Diff; comments: Comment[]; onComment: (c: Comment) => void; onDrop: (c: Comment) => void }) {
  const files = useMemo(() => parse(diff?.patch ?? ""), [diff?.patch]);
  const [open, setOpen] = useState<string | null>(null);
  const t = totals(files);
  const cur = open ?? files[0]?.path;
  return (
    <section aria-label="Changes">
      <h2>
        Changes<span class="sp" />
        {!!files.length && <><span class="plus">+{t.add}</span><span class="minus">−{t.del}</span></>}
      </h2>
      <div class="card">
        {!files.length && <p class="hint">No changes in this session's folder yet. They show after each turn.</p>}
        {files.map((f) => (
          <>
            <button class="file" aria-expanded={cur === f.path} onClick={() => setOpen(cur === f.path ? "" : f.path)} title={f.path}>
              <I n="file" />
              <span class="n">{short(f.path)}</span>
              <span class="plus">+{f.add}</span>
              <span class="minus">−{f.del}</span>
            </button>
            {cur === f.path && <FileDiff f={f} comments={comments} onComment={onComment} onDrop={onDrop} />}
          </>
        ))}
        {diff?.truncated && <p class="hint">The diff is cut at 64 KB. Run <code>git diff</code> in the folder for the rest.</p>}
      </div>
    </section>
  );
}

export function Decisions({ list, sessions }: { list: Dec[]; sessions: Record<string, Sess> }) {
  const open = list.filter((d) => d.answer == null);
  const done = list.filter((d) => d.answer != null).slice(-3).reverse();
  return (
    <section aria-label="Decisions">
      <h2>Decisions<span class="sp" /><span class="mono t3">{open.length} open</span></h2>
      <div class="card">
        {!list.length && <p class="hint">Nothing waits on you.</p>}
        {[...open, ...done].map((d) => {
          const q = split(d.question);
          const s = sessions[d.session];
          return (
            <a class="row2" href={s ? href({ name: "session", id: s.id }) : "#/decisions"}>
              <div class="top2">
                <St s={d.answer == null ? "needs_you" : "idle"} word={d.answer == null ? undefined : "answered"} />
                <span class="sp" />
                <span class="mono t3">{ago(d.asked)}</span>
              </div>
              <span class={d.answer == null ? "" : "t2"}>
                {q.code ? <code>{q.code}</code> : q.title}
                {d.answer != null && ": " + label(d.answer)}
              </span>
              {s && <span class="t3" style="font-size:12px">{s.title}</span>}
            </a>
          );
        })}
      </div>
    </section>
  );
}

export function Agents({ sessions, host }: { sessions: Sess[]; host: string }) {
  const live = sessions.filter((s) => s.status === "working" || s.status === "needs_you");
  return (
    <section aria-label="Agents">
      <h2>Agents<span class="sp" /><span class="mono t3">{host}</span></h2>
      <div class="card">
        {!live.length && <p class="hint">No agent is working right now.</p>}
        {live.map((s) => (
          <a class="row2" href={href({ name: "session", id: s.id })}>
            <div class="top2">
              <span style="font-weight:500">{agentName(s.agent)}</span>
              <span class="mono t3">{s.model}</span>
              <span class="sp" />
              <St s={s.status} />
            </div>
            <span class="t3" style="font-size:12px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{s.title}</span>
          </a>
        ))}
      </div>
    </section>
  );
}
