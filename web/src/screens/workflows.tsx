import { useEffect, useState } from "preact/hooks";
import { useLive } from "../lib/live";
import { loadWorkflow, loadWorkflows, ms, result, runWorkflow, saveWorkflow, summary, validateWorkflow, type RunRecord, type Step, type StepResult, type WorkflowDetail, type WorkflowList } from "../lib/workflows";

const when = (t?: string) => (t ? new Date(t).toLocaleString([], { weekday: "short", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }) : "");

function Steps({ steps }: { steps: Step[] }) {
  return (
    <ol style="margin:0;padding-left:20px">
      {steps.map((s) => (
        <li>
          <code>{s.kind}</code> {s.id} <span class="t3">{summary(s)}</span>
          {s.do && <Steps steps={s.do} />}
        </li>
      ))}
    </ol>
  );
}

export function Results({ steps }: { steps: StepResult[] }) {
  return (
    <ul style="margin:4px 0;padding-left:20px">
      {steps.map((s) => (
        <li>
          <code>{s.id}</code> <span class={s.status === "failed" ? "err" : "t3"}>{s.status}</span> <span class="t3">{ms(s.duration_ms)}{s.attempts ? ` · ${s.attempts} tries` : ""}</span> {result(s)}
          {(s.input || s.output) && s.kind !== "iteration" && (
            <details>
              <summary class="t3">input and output</summary>
              {s.input && <pre style="white-space:pre-wrap;max-height:200px;overflow:auto">{s.input}</pre>}
              {s.output && <pre style="white-space:pre-wrap;max-height:240px;overflow:auto">{s.output}</pre>}
            </details>
          )}
          {s.steps && <Results steps={s.steps} />}
        </li>
      ))}
    </ul>
  );
}

export const runHead = (r: RunRecord) => `${when(r.started)} · ${r.status} · ${r.trigger ?? ""}${r.schedule ? ":" + r.schedule : ""}${r.cost_usd ? ` · $${r.cost_usd.toFixed(2)}` : ""}${r.error ? " · " + r.error : ""}`;

// Workflows lists the workflows: run one (a form for its inputs), watch the run live, read past
// runs step by step, or edit its file with validation. Schedules start them on their own.
export default function Workflows() {
  const [l, setL] = useState<WorkflowList | null>(null);
  const [open, setOpen] = useState("");
  const [d, setD] = useState<WorkflowDetail | null>(null);
  const [edit, setEdit] = useState<string | null>(null);
  const [check, setCheck] = useState("");
  const [err, setErr] = useState("");
  const [form, setForm] = useState<{ name: string; inputs: Record<string, string> } | null>(null);
  const notes = useLive().store.notes.slice(-5).reverse();

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
  useEffect(() => {
    if (!open || !d?.running) return;
    const t = setInterval(() => detail(open), 2000);
    return () => clearInterval(t);
  }, [open, d?.running]);
  useEffect(() => {
    if (edit === null) return setCheck("");
    const t = setTimeout(() => validateWorkflow(edit).then((v) => setCheck(v.ok ? "" : v.error ?? "invalid")).catch(() => {}), 300);
    return () => clearTimeout(t);
  }, [edit]);

  const act = async (f: () => Promise<unknown>) => {
    setErr("");
    try {
      await f();
    } catch (e) {
      setErr((e as Error).message);
    }
    reload();
    if (open) setTimeout(() => detail(open), 300);
  };

  if (!l) return <section class="page"><p class="t3">{err || "Reading the workflows…"}</p></section>;
  return (
    <section class="page" aria-label="Workflows">
      <header>
        <h2>Workflows</h2>
        <p>{l.workflows.length ? `${l.workflows.length} workflow${l.workflows.length === 1 ? "" : "s"}${l.running.length ? `, ${l.running.length} running` : ""}.` : "No workflows yet."}</p>
      </header>
      {err && <p class="err" role="alert">{err}</p>}
      {!!notes.length && (
        <div aria-label="Notifications">
          <div class="lbl">Notifications</div>
          {notes.map((n) => (
            <div class="card" style="padding:8px 16px;margin-top:6px">
              <strong>{n.title}</strong> <span class="t3">{n.workflow} · {when(n.at)}</span>
              <p class="hint" style="white-space:pre-wrap">{n.text}</p>
            </div>
          ))}
        </div>
      )}
      {!l.workflows.length && (
        <div class="card"><p class="hint" style="padding:16px">
          A workflow is a JSON file in <code>~/.smithy/workflows</code>: steps that run commands, ask an agent or you, branch, loop and notify. Run it here; a schedule runs it on a cron, webhook, GitHub event or file change. <code>smithy workflow example</code> adds three.
        </p></div>
      )}
      {l.workflows.map((w) => {
        const running = l.running.includes(w.name);
        const st = running ? "running" : w.error ? "error" : w.last?.status ?? "";
        return (
          <div class="card" style="padding:12px 16px;display:flex;flex-direction:column;gap:8px">
            <div style="display:flex;gap:12px;align-items:baseline;flex-wrap:wrap">
              <strong>{w.name}</strong>
              <span class={st === "error" ? "err" : "t3"}>{st}</span>
              {!!w.schedules?.length && <a class="t3" href="#/schedules">schedules: {w.schedules.join(", ")}</a>}
              {w.last && <span class="t3">last {w.last.status} {when(w.last.started)}</span>}
              <span class="btns" style="margin-left:auto">
                <button class="btn sm primary" disabled={running || !!w.error} onClick={() => (w.inputs && Object.keys(w.inputs).length ? setForm({ name: w.name, inputs: { ...w.inputs } }) : act(async () => { await runWorkflow(w.name); setOpen(w.name); }))}>Run</button>
                <button class="btn sm" onClick={() => setOpen(open === w.name ? "" : w.name)} aria-expanded={open === w.name}>{open === w.name ? "Close" : "Open"}</button>
              </span>
            </div>
            {w.description && <p class="hint">{w.description}</p>}
            {w.error && <p class="err">{w.error}</p>}
            {form?.name === w.name && (
              <form aria-label={`Inputs for ${w.name}`} style="display:flex;flex-direction:column;gap:6px" onSubmit={(e) => { e.preventDefault(); const f = form; setForm(null); act(async () => { await runWorkflow(f.name, f.inputs); setOpen(f.name); }); }}>
                {Object.keys(form.inputs).sort().map((k) => (
                  <label style="display:flex;gap:8px;align-items:center">
                    <span class="lbl" style="min-width:120px">{k}</span>
                    <input style="flex:1" value={form.inputs[k]} onInput={(e) => setForm({ ...form, inputs: { ...form.inputs, [k]: e.currentTarget.value } })} />
                  </label>
                ))}
                <span class="btns"><button class="btn sm primary" type="submit">Run {w.name}</button><button class="btn sm" type="button" onClick={() => setForm(null)}>Cancel</button></span>
              </form>
            )}
            {open === w.name && d && (
              <div style="display:flex;flex-direction:column;gap:8px">
                {d.live && (
                  <div aria-label="Running now" aria-live="polite">
                    <div class="lbl">Running now</div>
                    <Results steps={d.live.steps} />
                  </div>
                )}
                <div class="lbl">Steps</div>
                <Steps steps={d.info.steps ?? []} />
                <div class="lbl">Runs</div>
                {!d.history.length && <p class="t3">No runs yet.</p>}
                {d.history.map((r) => (
                  <details>
                    <summary>{runHead(r)}</summary>
                    {r.outputs && Object.keys(r.outputs).length > 0 && <p class="hint">outputs: {Object.entries(r.outputs).map(([k, v]) => `${k}=${v}`).join(", ")}</p>}
                    <Results steps={r.steps} />
                  </details>
                ))}
                {edit === null ? (
                  <span class="btns"><button class="btn sm" onClick={() => setEdit(d.source)}>Edit</button></span>
                ) : (
                  <div style="display:flex;flex-direction:column;gap:6px">
                    <textarea aria-label={`Edit ${w.name}`} aria-invalid={!!check} rows={18} spellcheck={false} style="font-family:var(--mono, monospace);width:100%" value={edit} onInput={(e) => setEdit(e.currentTarget.value)} />
                    <p class={check ? "err" : "hint"} role="status">{check || "Valid."}</p>
                    <span class="btns">
                      <button class="btn sm primary" disabled={!!check} onClick={() => act(async () => { await saveWorkflow(w.name, edit); setEdit(null); })}>{w.schedules?.length ? "Save (turns its schedules off)" : "Save"}</button>
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
