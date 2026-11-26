import { useEffect, useRef, useState } from "preact/hooks";
import "./folderpicker.css";
import { I } from "./icons";
import { crumbs, listDirs, type DirList } from "./projects";

// FolderPicker browses the daemon machine's folders (GET /v1/fs) and hands back the one chosen,
// so nobody types a path from memory.
export default function FolderPicker({ open, host, start, onPick, onClose }: { open: boolean; host: string; start?: string; onPick: (dir: string, def: boolean) => Promise<string>; onClose: () => void }) {
  const ref = useRef<HTMLDialogElement>(null);
  const [list, setList] = useState<DirList | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [def, setDef] = useState(false);
  const [q, setQ] = useState("");

  const go = (dir = "") => {
    setErr("");
    setQ("");
    listDirs(dir)
      .then(setList)
      .catch((e) => setErr(String(e.message ?? e)));
  };
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) {
      d.showModal();
      setDef(false);
      go(start ?? "");
    } else if (!open && d.open) d.close();
  }, [open]);

  const pick = async () => {
    if (!list) return;
    setBusy(true);
    const e = await onPick(list.dir, def).catch((x) => String(x.message ?? x));
    setBusy(false);
    if (e) setErr(e);
    else onClose();
  };
  const dirs = (list?.dirs ?? []).filter((d) => d.toLowerCase().includes(q.trim().toLowerCase()));
  const sep = list && /^[A-Za-z]:\\/.test(list.dir) ? "\\" : "/";
  const join = (d: string) => (list!.dir.endsWith(sep) ? list!.dir + d : list!.dir + sep + d);

  return (
    <dialog ref={ref} class="fpick" aria-label="Add a project" onClose={onClose} onCancel={onClose}>
      <header>
        <b>Add a project</b>
        <span class="t3"><I n="server" />{host}</span>
        <span class="sp" />
        <button class="btn sm" onClick={onClose} aria-label="Close">esc</button>
      </header>
      <nav class="crumbs" aria-label="Path">
        {list && crumbs(list.dir).map((c, i, a) => (
          <>
            <button class={"mono" + (i === a.length - 1 ? " cur" : "")} onClick={() => go(c.path)} disabled={i === a.length - 1}>{c.label}</button>
            {i > 0 && i < a.length - 1 && <span class="t3">/</span>}
          </>
        ))}
      </nav>
      <div class="filter">
        <I n="search" />
        <input value={q} onInput={(e) => setQ(e.currentTarget.value)} placeholder="Filter folders" aria-label="Filter folders" spellcheck={false} autoComplete="off" />
      </div>
      <ul class="dirs" role="listbox" aria-label="Folders">
        {list?.parent && (
          <li><button onClick={() => go(list.parent)}><I n="up" /><span class="mono">..</span></button></li>
        )}
        {dirs.map((d) => (
          <li><button onClick={() => go(join(d))}><I n="folder" /><span>{d}</span></button></li>
        ))}
        {list && !dirs.length && <li class="t3 none">{q ? "No folder matches." : "No folders here."}</li>}
        {!list && !err && <li class="t3 none">Reading folders…</li>}
      </ul>
      {err && <p class="err" role="alert">{err}</p>}
      <footer>
        <label class="t2"><input type="checkbox" checked={def} onChange={(e) => setDef(e.currentTarget.checked)} /> Make it the default</label>
        <span class="sp" />
        <button class="btn primary" disabled={!list || busy} onClick={pick}>
          {busy ? "Adding…" : "Add " + (list ? crumbs(list.dir).pop()!.label : "folder")}
        </button>
      </footer>
    </dialog>
  );
}
