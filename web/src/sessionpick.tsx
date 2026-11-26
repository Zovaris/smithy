import { Dialog } from "@zovaris/sephiro/dialog";
import { SegmentedControl } from "@zovaris/sephiro/segmented-control";
import "@zovaris/sephiro/css/dialog.css";
import "@zovaris/sephiro/css/segmented-control.css";
import "./screens/settings.css";
import { useState } from "preact/hooks";
import { setSessionModel } from "./actions";
import { ModelSelect } from "./modelselect";
import { effortOptions, modelEditable } from "./parity";
import { agentName, type Sess } from "./store";
import { Keys } from "./keys";

// SessionPick is /model and /effort: the open session's model and reasoning effort, for its next
// turns. Effort shows only for agents that take one (claude, codex), capped at medium as in the TUI.
export default function SessionPick({ s, open, onClose }: { s: Sess; open: boolean; onClose: () => void }) {
  const [model, setModel] = useState(s.model ?? "");
  const [effort, setEffort] = useState(s.effort ?? "");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const efforts = effortOptions(s.agent);
  const editable = modelEditable(s.agent);
  const save = async () => {
    setBusy(true);
    setErr("");
    try {
      await setSessionModel(s.id, model, efforts.length ? effort : "");
      onClose();
    } catch (e) {
      setErr(String((e as Error).message ?? e));
    }
    setBusy(false);
  };
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={"Model for " + agentName(s.agent)}
      description="This session's next turns; new sessions keep the default from Settings."
      footer={
        <div class="dlg-foot">
          <Keys k={[["enter", "save"], ["esc", "cancel"]]} />
          <span class="sp" />
          <button class="btn" onClick={onClose}>Cancel</button>
          <button class="btn primary" disabled={busy || !editable} onClick={save}>{busy ? "Saving…" : "Save"}</button>
        </div>
      }
    >
      <form class="pick" onSubmit={(e) => (e.preventDefault(), save())}>
        <div class="lbl">Model</div>
        {editable ? (
          <ModelSelect agent={{ name: s.agent.replace(/^acp:/, ""), kind: "cli", ready: true, detail: "" }} value={model} onChange={setModel} id="session-model" label="Model" />
        ) : (
          <p class="t3">An API session's model is part of its agent; start a new session for another one.</p>
        )}
        <div class="lbl">Effort</div>
        {efforts.length ? (
          <SegmentedControl
            ariaLabel="Reasoning effort"
            value={effort}
            onValueChange={setEffort}
            options={efforts.map((e) => ({ value: e, label: e || "default" }))}
          />
        ) : (
          <p class="t3">{agentName(s.agent)} keeps its own effort; effort works for Claude Code and Codex.</p>
        )}
        {!!efforts.length && <p class="t3">smithy caps effort at medium; default follows the agent.effort setting.</p>}
        {err && <p class="err" role="alert">{err}</p>}
      </form>
    </Dialog>
  );
}
