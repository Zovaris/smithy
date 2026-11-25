import { useEffect, useRef, useState } from "preact/hooks";
import { loadModels, modelRows, type ModelList, type Ready } from "./setup";

// ModelSelect is the Model field: a searchable list of what the agent reports, its default
// first, and Other (the typed text) only when nothing matches.
export function ModelSelect({ agent, value, onChange, id = "model", label }: { agent: Ready; value: string; onChange: (m: string) => void; id?: string; label?: string }) {
  const [list, setList] = useState<ModelList | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const [sel, setSel] = useState(0);
  const box = useRef<HTMLDivElement>(null);

  const fetch = (refresh: boolean) => {
    setBusy(true);
    setErr("");
    loadModels(agent.name, refresh)
      .then((l) => {
        setList(l);
        setErr(l.error ?? "");
      })
      .catch((e) => setErr(String(e.message ?? e)))
      .finally(() => setBusy(false));
  };
  useEffect(() => {
    setList(null);
    fetch(false);
  }, [agent.name]);
  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => !box.current?.contains(e.target as Node) && setOpen(false);
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [open]);

  const rows = modelRows(list, q, agent.kind);
  const pick = (i: number) => {
    const r = rows[i];
    if (!r) return;
    onChange(r.id);
    setQ("");
    setOpen(false);
  };
  const shown = value || (agent.kind === "api" ? "" : "agent default");
  const key = (e: KeyboardEvent) => {
    if (e.key === "ArrowDown") (e.preventDefault(), setOpen(true), setSel(Math.min(sel + 1, rows.length - 1)));
    else if (e.key === "ArrowUp") (e.preventDefault(), setSel(Math.max(sel - 1, 0)));
    else if (e.key === "Enter" && open) (e.preventDefault(), pick(sel));
    else if (e.key === "Escape") setOpen(false);
  };

  return (
    <div class="msel" ref={box}>
      <input
        id={id}
        role="combobox"
        aria-label={label}
        aria-expanded={open}
        aria-controls={id + "-list"}
        aria-autocomplete="list"
        value={open ? q : shown}
        placeholder={agent.kind === "api" ? "pick a model" : "agent default"}
        onFocus={() => (setOpen(true), setQ(""), setSel(0))}
        onInput={(e) => (setQ(e.currentTarget.value), setSel(0), setOpen(true))}
        onKeyDown={key}
        spellcheck={false}
        autoComplete="off"
      />
      <button class="t3 refresh" title="Ask the agent for its models again" aria-label="Refresh models" disabled={busy} onClick={() => fetch(true)}>
        {busy ? "…" : "↻"}
      </button>
      {open && (
        <ul id={id + "-list"} role="listbox" class="msel-list">
          {rows.map((r, i) => (
            <li
              role="option"
              aria-selected={i === sel}
              class={(i === sel ? "on" : "") + (r.other ? " other" : "")}
              onMouseDown={(e) => (e.preventDefault(), pick(i))}
              onMouseEnter={() => setSel(i)}
            >
              <span class="mono">{r.label}</span>
              {r.isDefault && <span class="def">default</span>}
              <span class="sp" />
              <span class="t3">{r.hint}</span>
            </li>
          ))}
          {!rows.length && <li class="t3">{busy ? "Asking the agent…" : "Type a model name"}</li>}
          <li class="t3 src" aria-hidden="true">{busy && !list ? "Asking " + agent.name + " for its models…" : list ? list.models.length + " from " + list.source : ""}</li>
        </ul>
      )}
      {err && <span class="t3 msel-err" title={err}>{err}</span>}
    </div>
  );
}
