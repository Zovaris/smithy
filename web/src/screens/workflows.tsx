import { useEffect, useState } from "preact/hooks";
import { loadWorkflow, loadWorkflows, result, runWorkflow, saveWorkflow, setWorkflow, state, type WorkflowDetail, type WorkflowList } from "../lib/workflows";

const when = (t?: string) => (t ? new Date(t).toLocaleString([], { weekday: "short", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }) : "");

// Workflows lists the scheduled workflows the daemon runs: run one now, turn its schedule on or
// off, read its runs with each step's output, or edit its file.
export default function Workflows() {
  const [l, setL] = useState<WorkflowList | null>(null);
  const [open, setOpen] = useState("");
  const [d, setD] = useState<WorkflowDetail | null>(null);
  const [edit, setEdit] = useState<string | null>(null);
  const [err, setErr] = useState("");

  const reload = () => loadWorkflows().then(setL).catch((e) => setErr(String(e.message ?? e)));
  const detail = (name: string) => loadWorkflow(name).then(setD).catch((e) => setErr(String(e.message ?? e)));
  useEffect(() => {
    reload();
    const t = setInterval(reload, 10000);
    return () => clearInterval(t);
  }, []);
  useEffect(() => {
    setD(null);
    setEdit(null);
    if (open) detail(open);
  }, [open]);

  const act = async (f: () => Promise<unknown>) => {
    setErr("");
    try {
      await f();
    } catch (e) {
      setErr((e as Error).message);
    }
    reload();
    if (open) detail(open);
  };

  if (!l) return <section class="page"><p class="t3">{err || "Reading the workflows…"}</p></section>;
  return (
    <section class="page" aria-label="Workflows">
      <header>
        <h2>Workflows</h2>
        <p>{l.workflows.length ? `${l.workflows.filter((w) => w.enabled).length} of ${l.workflows.length} on a schedule.` : "No workflows yet."}</p>
      </header>
      {err && <p class="err" role="alert">{err}</p>}
      {!l.workflows.length && (
        <div class="card"><p class="hint" style="padding:16px">
          A workflow is a JSON file in <code>~/.smithy/workflows</code>: a cron schedule and steps that run a command, ask an agent, branch and notify you. <code>smithy workflow example</code> adds a daily Dependabot check.
        </p></div>
      )}
      {l.workflows.map((w) => {
        const st = state(w, l.running);
        return (
          <div class="card" style="padding:12px 16px;display:flex;flex-direction:column;gap:8px">
            <div style="display:flex;gap:12px;align-items:baseline;flex-wrap:wrap">
              <strong>{w.name}</strong>
              <span class="t3">{st}</span>
              <code>{w.schedule}</code>
              {w.next && <span class="t3">next {when(w.next)}</span>}
              {w.last && <span class="t3">last {w.last.status} {when(w.last.started)}</span>}
              <span class="btns" style="margin-left:auto">
                <button class="btn sm" disabled={st === "running"} onClick={() => act(() => runWorkflow(w.name))}>Run now</button>
                <button class="btn sm" disabled={!!w.error} onClick={() => act(() => setWorkflow(w.name, !w.enabled))}>{w.enabled ? "Turn off" : "Turn on"}</button>
                <button class="btn sm" onClick={() => setOpen(open === w.name ? "" : w.name)} aria-expanded={open === w.name}>{open === w.name ? "Close" : "History"}</button>
              </span>
            </div>
            {w.description && <p class="hint">{w.description}</p>}
            {w.error && <p class="err">{w.error}</p>}
            {w.changed && <p class="hint">The file changed outside smithy since it was turned on; turn it on again to run it on schedule.</p>}
            {open === w.name && d && (
              <div style="display:flex;flex-direction:column;gap:8px">
                <div class="lbl">Steps</div>
                <ol style="margin:0;padding-left:20px">
                  {d.info.steps?.map((s) => <li><code>{s.kind}</code> {s.id} <span class="t3">{s.run ?? s.prompt ?? (s.step ? "on " + s.step : s.message) ?? ""}</span></li>)}
                </ol>
                <div class="lbl">Runs</div>
                {!d.history.length && <p class="t3">No runs yet.</p>}
                {d.history.map((r) => (
                  <details>
                    <summary>{when(r.started)} · {r.status} · {r.trigger}{r.cost_usd ? ` · $${r.cost_usd.toFixed(2)}` : ""}{r.error ? " · " + r.error : ""}</summary>
                    <ul style="margin:4px 0;padding-left:20px">
                      {r.steps.map((s) => <li><code>{s.id}</code> {result(s)}{s.output && s.kind !== "if" && <pre style="white-space:pre-wrap;max-height:240px;overflow:auto">{s.output}</pre>}</li>)}
                    </ul>
                  </details>
                ))}
                {edit === null ? (
                  <span class="btns"><button class="btn sm" onClick={() => setEdit(d.source)}>Edit</button></span>
                ) : (
                  <div style="display:flex;flex-direction:column;gap:6px">
                    <textarea aria-label={`Edit ${w.name}`} rows={16} spellcheck={false} style="font-family:var(--mono, monospace);width:100%" value={edit} onInput={(e) => setEdit(e.currentTarget.value)} />
                    <span class="btns">
                      <button class="btn sm primary" onClick={() => act(async () => { await saveWorkflow(w.name, edit); setEdit(null); })}>Save</button>
                      <button class="btn sm" onClick={() => setEdit(null)}>Cancel</button>
                    </span>
                  </div>
                )}
              </div>
            )}
          </div>
        );
      })}
    </section>
  );
}
