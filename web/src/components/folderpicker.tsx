import { Dialog } from "@zovaris/sephiro/dialog";
import { FileBrowser } from "@zovaris/sephiro/file-browser";
import "@zovaris/sephiro/css/dialog.css";
import "@zovaris/sephiro/css/file-browser.css";
import { useEffect, useState } from "preact/hooks";
import { listDirs, type DirList } from "../lib/projects";

// FolderPicker browses the daemon machine's folders (GET /v1/fs) and hands back the one chosen,
// so nobody types a path from memory.
export default function FolderPicker({ open, host, start, onPick, onClose }: { open: boolean; host: string; start?: string; onPick: (dir: string, def: boolean) => Promise<string>; onClose: () => void }) {
  const [list, setList] = useState<DirList | null>(null);
  const [listErr, setListErr] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [def, setDef] = useState(false);

  const go = (dir = "") => {
    setErr("");
    setListErr("");
    listDirs(dir)
      .then(setList)
      .catch((e) => setListErr(String(e.message ?? e)));
  };
  useEffect(() => {
    if (!open) return;
    setDef(false);
    go(start ?? "");
  }, [open]);

  const pick = async (dir: string) => {
    if (busy) return;
    setBusy(true);
    const e = await onPick(dir, def).catch((x) => String(x.message ?? x));
    setBusy(false);
    if (e) setErr(e);
    else onClose();
  };
  const name = list ? (list.dir.split(/[\\/]/).filter(Boolean).pop() ?? list.dir) : "folder";

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Add a project"
      description={host}
      escHint
      footer={<label class="t2"><input type="checkbox" checked={def} onChange={(e) => setDef(e.currentTarget.checked)} /> Make it the default</label>}
    >
      <FileBrowser
        path={list?.dir ?? ""}
        entries={(list?.dirs ?? []).map((d) => ({ name: d, kind: "dir" as const }))}
        onNavigate={go}
        onPick={pick}
        loading={!list && !listErr}
        error={listErr || undefined}
        dirsOnly
        label="Folders"
        labels={{ filter: "Filter folders", pick: busy ? "Adding…" : "Add " + name, loading: "Reading folders…", empty: "No folders here.", noMatch: "No folder matches." }}
      />
      {err && <p class="err" role="alert">{err}</p>}
    </Dialog>
  );
}
