import { useState } from "preact/hooks";
import { startSession } from "../../lib/live";
import { go } from "../../lib/router";
import { refined } from "../../lib/refine";
import type { Item, Sess } from "../../lib/store";

// Refined offers the prompt the refine skill ended with: copy it, or open a new session with it.
export default function Refined({ s, items = [] }: { s: Sess; items?: Item[] }) {
  const text = s.status === "working" ? "" : refined(items);
  const [note, setNote] = useState("");
  const copy = () => navigator.clipboard?.writeText(text).then(() => setNote("Copied"), () => setNote("Copy failed"));
  const open = () =>
    startSession({ agent: s.agent, model: s.model ?? "", effort: s.effort, dir: s.dir, message: text }).then(
      (n) => go({ name: "session", id: n.id }),
      (e) => setNote(String(e?.message ?? e)),
    );
  if (!text) return null;
  return (
    <div class="row1 refined" role="group" aria-label="Refined prompt">
      <b>Refined prompt ready</b>
      <span class="sp" />
      {note && <span class="t3">{note}</span>}
      <button class="btn sm" onClick={copy}>Copy</button>
      <button class="btn sm primary" onClick={open}>New session</button>
    </div>
  );
}
