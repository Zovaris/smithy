import { useEffect, useState } from "preact/hooks";
import { issueHint, loadLinks, prHint, prTone, type Links } from "../lib/links";

// SessionLinks is the session's linked work in the side panel: its PRs and issues, kept live by
// the daemon's session_links events (read once here when none has arrived yet).
export function SessionLinks({ id, links }: { id: string; links?: Links }) {
  const [got, set] = useState<Links | undefined>();
  const [err, setErr] = useState("");
  useEffect(() => {
    set(undefined);
    setErr("");
    if (!links) loadLinks(id).then(set, (e) => setErr(String((e as Error).message ?? e)));
  }, [id]);
  const x = links ?? got;
  const l = x && { ...x, prs: x.prs ?? [], issues: x.issues ?? [] };
  const none = l && !l.prs.length && !l.issues.length;
  return (
    <section aria-label="Links" id="links">
      <h2>Links<span class="sp" />{l && <span class="mono t3">{l.prs.length} PR · {l.issues.length} issue{l.issues.length === 1 ? "" : "s"}</span>}</h2>
      <div class="card">
        {err && <p class="hint err">{err}</p>}
        {!l && !err && <p class="hint">Loading…</p>}
        {none && <p class="hint">No PR or issue yet. Name one in the branch, a PR or a prompt, or <code>/links TK-123</code>.</p>}
        {l?.prs.map((p) => (
          <div class="row2">
            <div class="top2">
              <a class="tt1 mono" href={p.url} target="_blank" rel="noreferrer">#{p.number}</a>
              <span class="t3">{p.title}</span>
              <span class="sp" />
              <span class={"st " + prTone(p)}>{p.checks || p.state.toLowerCase()}</span>
            </div>
            <span class="t3" style="font-size:12px">{prHint(p)}</span>
          </div>
        ))}
        {l?.issues.map((i) => (
          <div class="row2">
            <div class="top2">
              <a class="tt1 mono" href={i.url} target="_blank" rel="noreferrer">{i.id}</a>
              <span class="t3">{i.title}</span>
            </div>
            <span class="t3" style="font-size:12px">{issueHint(i)}</span>
          </div>
        ))}
        {l?.error && <p class="hint err">{l.error}</p>}
      </div>
    </section>
  );
}
