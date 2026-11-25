import { useState } from "preact/hooks";
import { Button } from "@zovaris/sephiro/button";
import "@zovaris/sephiro/css/button.css";
import { glyph, type Decision } from "./model";
import { decide } from "./live";

export const G = ({ s }: { s?: string }) => (
  <span class={"g " + (s ?? "ok")} aria-label={s ?? "ok"}>
    {glyph(s)}
  </span>
);

export const choices = (d: Decision) => (d.options?.length ? d.options : ["yes", "no"]);

// Answer is one decision with a button per option; the daemon keeps the first answer only.
export function Answer({ d, compact }: { d: Decision; compact?: boolean }) {
  const [busy, setBusy] = useState("");
  const [err, setErr] = useState("");
  const go = async (a: string) => {
    setBusy(a);
    setErr(await decide(d.id, a).catch((e) => String(e)));
    setBusy("");
  };
  return (
    <div class={compact ? "ask compact" : "ask"} role="group" aria-label={d.question}>
      <span class="chip" aria-hidden="true">?</span>
      <span class="q">{d.question}</span>
      <span class="opts">
        {choices(d).map((o) => (
          <Button size="sm" variant="quiet" loading={busy === o} disabled={!!busy} onClick={() => go(o)} aria-keyshortcuts={compact ? o[0] : undefined}>
            <kbd>{o[0]}</kbd> {o}
          </Button>
        ))}
      </span>
      {err && <span class="g failed" role="alert">✗ {err}</span>}
    </div>
  );
}
