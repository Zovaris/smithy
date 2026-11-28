import { useEffect, useState } from "preact/hooks";
import { issueHint, loadWork, prHint, prTone, type Work as W } from "../lib/links";
import { loadProjects, projectOf, type ProjectList } from "../lib/projects";
import { href } from "../lib/router";

// Work is a project's linked work across its sessions on the Agents page: open PRs and linked
// issues, each with the sessions that link it. The daemon keeps links live; this reads them every 15s.
export function Work() {
  const [pl, setPl] = useState<ProjectList | null>(null);
  const [dir, setDir] = useState("");
  const [w, set] = useState<W | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    loadProjects().then((l) => (setPl(l), setDir((d) => d || l.default || "")), () => setPl({ projects: [] }));
  }, []);
  useEffect(() => {
    const get = () => loadWork(dir).then((x) => (set(x), setErr("")), (e) => setErr(String((e as Error).message ?? e)));
    get();
    const i = setInterval(get, 15000);
    return () => clearInterval(i);
  }, [dir]);
  const sess = (ids: string[]) => ids.map((id) => <a class="mono" href={href({ name: "session", id })}> {id}</a>);
  return (
    <div id="work" aria-label="Work">
      <div class="lbl">Work<span class="sp" />
        {pl && pl.projects.length > 0 ? (
          <select aria-label="Project" value={dir} onChange={(e) => setDir((e.target as HTMLSelectElement).value)}>
            <option value="">All projects</option>
            {pl.projects.map((p) => <option value={p.dir}>{projectOf(pl, p.dir)}</option>)}
          </select>
        ) : <span class="mono">{w ? w.prs.length + " open PRs · " + w.issues.length + " issues" : ""}</span>}
      </div>
      <div class="card">
        {err && <p class="hint err">{err}</p>}
        {w && !w.prs.length && !w.issues.length && <p class="hint">No open PR or linked issue yet. Sessions link the PRs they open and the issues named in their branch, PRs or prompts, or with <code>/links TK-123</code>.</p>}
        {w?.prs.map((p) => (
          <div class="row2">
            <div class="top2">
              <a class="tt1 mono" href={p.url} target="_blank" rel="noreferrer">#{p.number}</a>
              <span class="t3">{p.title}</span>
              <span class="sp" />
              <span class={"st " + prTone(p)}>{p.checks || p.state.toLowerCase()}</span>
            </div>
            <span class="t3" style="font-size:12px">{prHint(p)} ·{sess(p.sessions)}</span>
          </div>
        ))}
        {w?.issues.map((i) => (
          <div class="row2">
            <div class="top2">
              <a class="tt1 mono" href={i.url} target="_blank" rel="noreferrer">{i.id}</a>
              <span class="t3">{i.title}</span>
            </div>
            <span class="t3" style="font-size:12px">{issueHint(i)} ·{sess(i.sessions)}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
