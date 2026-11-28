import { useEffect, useState } from "preact/hooks";
import { loadSchedule, loadSchedules, setSchedule, state, trigger, type Schedule, type ScheduleDetail } from "../lib/workflows";
import { Results, runHead } from "./workflows";

const when = (t?: string) => (t ? new Date(t).toLocaleString([], { weekday: "short", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }) : "");

// Schedules lists what starts workflows on its own: a cron line, a webhook, a GitHub event or a
// file change. Turning one on pins its file and its workflow; a later change turns it off.
export default function Schedules() {
  const [l, setL] = useState<Schedule[] | null>(null);
  const [open, setOpen] = useState("");
  const [d, setD] = useState<ScheduleDetail | null>(null);
  const [err, setErr] = useState("");
  const reload = () => loadSchedules().then((r) => setL(r.schedules)).catch((e) => setErr(String(e.message ?? e)));
  useEffect(() => {
    reload();
    const t = setInterval(reload, 10000);
    return () => clearInterval(t);
  }, []);
  useEffect(() => {
    setD(null);
    if (open) loadSchedule(open).then(setD).catch((e) => setErr(String(e.message ?? e)));
  }, [open]);
  const toggle = async (s: Schedule) => {
    setErr("");
    try {
      await setSchedule(s.name, !s.enabled);
    } catch (e) {
      setErr((e as Error).message);
    }
    reload();
    if (open === s.name) loadSchedule(s.name).then(setD).catch(() => {});
  };

  if (!l) return <section class="page"><p class="t3">{err || "Reading the schedules…"}</p></section>;
  return (
    <section class="page" aria-label="Schedules">
      <header>
        <h2>Schedules</h2>
        <p>{l.length ? `${l.filter((s) => s.enabled).length} of ${l.length} on.` : "No schedules yet."}</p>
      </header>
      {err && <p class="err" role="alert">{err}</p>}
      {!l.length && (
        <div class="card"><p class="hint" style="padding:16px">
          A schedule starts a workflow on a cron line, a webhook, a GitHub event (a Dependabot alert, a red PR, a labeled issue) or a file change: <code>smithy schedule add nightly --workflow repo-health --cron "0 9 * * 1-5"</code>, then turn it on here.
        </p></div>
      )}
      {l.map((s) => {
        const st = state(s);
        return (
          <div class="card" style="padding:12px 16px;display:flex;flex-direction:column;gap:8px">
            <div style="display:flex;gap:12px;align-items:baseline;flex-wrap:wrap">
              <strong>{s.name}</strong>
              <span class={st === "error" ? "err" : "t3"}>{st}</span>
              <code>{trigger(s)}</code>
              <a class="t3" href="#/workflows">→ {s.workflow}</a>
              {s.next && <span class="t3">next {when(s.next)}</span>}
              {s.last && <span class="t3">last {s.last.status} {when(s.last.started)}</span>}
              <span class="btns" style="margin-left:auto">
                <button class="btn sm" disabled={!!s.error} onClick={() => toggle(s)}>{s.enabled ? "Turn off" : "Turn on"}</button>
                <button class="btn sm" onClick={() => setOpen(open === s.name ? "" : s.name)} aria-expanded={open === s.name}>{open === s.name ? "Close" : "History"}</button>
              </span>
            </div>
            {s.description && <p class="hint">{s.description}</p>}
            {s.error && <p class="err">{s.error}</p>}
            {s.changed && <p class="hint">Its file or its workflow changed since it was turned on; turn it on again to let it fire.</p>}
            {open === s.name && d && (
              <div style="display:flex;flex-direction:column;gap:8px">
                {d.hook && <p class="hint">Webhook: <code>POST /v1/hooks/schedules/{s.name}</code> with header <code>X-Smithy-Hook: {d.hook}</code> and a JSON object of inputs.</p>}
                {!d.history.length && <p class="t3">No runs yet.</p>}
                {d.history.map((r) => (
                  <details>
                    <summary>{runHead(r)}</summary>
                    <Results steps={r.steps} />
                  </details>
                ))}
              </div>
            )}
          </div>
        );
      })}
    </section>
  );
}
