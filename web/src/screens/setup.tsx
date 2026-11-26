import { useEffect, useState } from "preact/hooks";
import { api } from "../auth";
import { loadSetup as refresh } from "../live";
import { choice, installSkills, isDefault, loadSetup, type Ready, setDefault, setKit, type Setup as S, toggleSkill } from "../setup";
import { ModelSelect } from "../modelselect";
import { agentName } from "../store";
import { St } from "../ui";
import { More } from "./settingsparts";

type Guard = { enabled: boolean; block: string[]; allow: string[]; fetch_block: string[]; fetch_allow: string[]; suggested: { host: string; added: boolean }[] };

function AgentRow({ r, s, open, onOpen, onSaved }: { r: Ready; s: S; open: boolean; onOpen: () => void; onSaved: () => void }) {
  const cur = s.chosen && isDefault(r, s.default);
  const [model, setModel] = useState(cur ? (r.kind === "api" ? s.default.agent.split("/").slice(1).join("/") : s.default.model ?? "") : "");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const use = async (e: Event) => {
    e.preventDefault();
    const c = choice(r, model);
    if (!c) return setErr("An API agent needs a model.");
    setBusy(true);
    setErr("");
    try {
      await setDefault(c);
      onSaved();
    } catch (x) {
      setErr(String((x as Error).message ?? x));
    }
    setBusy(false);
  };
  return (
    <>
      <button class="row2" aria-expanded={r.ready ? open : undefined} onClick={onOpen} disabled={!r.ready} style={r.ready ? "" : "cursor:default"}>
        <div class="top2">
          <span style="font-weight:500">{agentName(r.name)}</span>
          {cur && <span class="tag" style="height:20px;font-size:11px;padding:0 6px">default{s.default.model ? " · " + s.default.model : ""}</span>}
          <span class="sp" />
          <St s={r.ready ? "ready" : "missing"} />
        </div>
        <span class="mono t3" style="font-size:12px">{r.ready ? r.detail : r.fix || r.detail}</span>
      </button>
      {open && r.ready && (
        <form class="model" onSubmit={use} style="padding-left:12px;flex-wrap:wrap">
          <ModelSelect agent={r} value={model} onChange={setModel} id={"model-" + r.name} label={`Model for ${agentName(r.name)}`} />
          <button class="btn sm primary" type="submit" disabled={busy}>{busy ? "Saving…" : "Use for new sessions"}</button>
          {err && <span class="err" role="alert">{err}</span>}
        </form>
      )}
    </>
  );
}

// Settings is first run and the agent/model picker (ctrl+l in the TUI), what goes into every
// session (smithy's MCP server and skills), what each harness has installed, and the guard.
export default function Setup() {
  const [s, setS] = useState<S | null>(null);
  const [g, setG] = useState<Guard | null>(null);
  const [err, setErr] = useState("");
  const [open, setOpen] = useState("");
  const [out, setOut] = useState("");
  const [busy, setBusy] = useState("");
  const load = () => {
    loadSetup().then(setS, (e) => setErr(String(e.message ?? e)));
    api("/v1/guard")
      .then((r) => (r.ok ? r.json() : null))
      .then((x) => x && setG({ enabled: !!x.enabled, block: x.block ?? [], allow: x.allow ?? [], fetch_block: x.fetch_block ?? [], fetch_allow: x.fetch_allow ?? [], suggested: x.suggested ?? [] }))
      .catch(() => {});
    refresh();
  };
  useEffect(load, []);
  if (err && !s) return <section class="page"><p class="err" role="alert">Could not read the agents: {err}</p></section>;
  if (!s) return <section class="page"><p class="t3">Reading the agents…</p></section>;
  const kit = async (k: S["kit"]) => {
    setS({ ...s, kit: k });
    await setKit(k).catch((e) => setErr(String((e as Error).message ?? e)));
    load();
  };
  const install = async () => {
    setBusy("install");
    setOut("Installing smithy skills into every detected harness…");
    setOut(await installSkills().then((r) => r.output.trim() || "Done.", (e) => "Install failed: " + String((e as Error).message ?? e)));
    setBusy("");
    load();
  };
  const addHosts = async () => {
    setBusy("hosts");
    await api("/v1/guard/suggested", { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" }).catch(() => {});
    setBusy("");
    load();
  };
  const ready = s.agents.filter((a) => a.ready).length;
  const rows = (list: Ready[]) => (
    <div class="card">
      {list.map((r) => <AgentRow key={r.name} r={r} s={s} open={open === r.name} onOpen={() => setOpen(open === r.name ? "" : r.name)} onSaved={() => { setOpen(""); load(); }} />)}
    </div>
  );
  const missing = g?.suggested.filter((h) => !h.added) ?? [];
  return (
    <section class="page" aria-label="Settings">
      <header>
        {s.chosen ? (
          <><h2>Settings</h2><p>{ready} of {s.agents.length} agents ready · new sessions start with {agentName(s.default.agent)}{s.default.model ? " · " + s.default.model : ""}</p></>
        ) : (
          <><h2>Welcome to smithy</h2><p>These are the agents on this machine. Pick the one new sessions start with; you can change it here or with <kbd>ctrl+l</kbd> in the TUI.</p></>
        )}
      </header>
      {err && <p class="err" role="alert">{err}</p>}
      <div id="set-agents"><div class="lbl">Agent CLIs</div>{rows(s.agents.filter((a) => a.kind === "cli"))}</div>
      <div><div class="lbl">API keys</div>{rows(s.agents.filter((a) => a.kind === "api"))}</div>
      <div id="set-skills">
        <div class="lbl">Injected into every session</div>
        <div class="card">
          <label class="kv">
            <input type="checkbox" checked={s.kit.mcp} onChange={() => kit({ ...s.kit, mcp: !s.kit.mcp })} />
            <span style="font-weight:500">smithy MCP server</span>
            <span class="t3">board, cards and decisions as tools</span>
          </label>
          {s.skills.map((k) => (
            <label class="kv">
              <input type="checkbox" checked={s.kit.skills.includes(k)} onChange={() => kit(toggleSkill(s.kit, k))} />
              <span class="mono" style="font-weight:500">{k}</span>
              <span class="t3">skill</span>
            </label>
          ))}
          {!s.skills.length && <p class="hint" style="padding:12px">No smithy skills installed yet.</p>}
        </div>
      </div>
      <div>
        <div class="lbl">Installed per harness</div>
        <div class="card scrollx">
          <table class="tbl">
            <thead><tr><th>Harness</th><th>smithy skills</th><th>Other skills</th><th>MCP servers</th></tr></thead>
            <tbody>
              {s.harnesses.map((h) => (
                <tr class={h.present ? "" : "t3"}>
                  <td style="white-space:nowrap">{h.present ? <St s={h.missing.length ? "needs_you" : "ready"} word={h.harness} /> : <St s="idle" word={h.harness} />}</td>
                  {h.present ? (
                    <>
                      <td title={h.missing.length ? "missing: " + h.missing.join(", ") : ""}>{h.smithy.length}/{h.smithy.length + h.missing.length}</td>
                      <td>{h.other.length}</td>
                      <td class={h.mcp_error ? "err" : "t2"}>{h.mcp_error ?? (h.mcp.join(", ") || "none")}</td>
                    </>
                  ) : (
                    <td colSpan={3}>not installed</td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div class="btns" style="margin-top:12px;align-items:center">
          <button class="btn" disabled={busy === "install"} onClick={install}>{busy === "install" ? "Installing…" : "Install or update smithy skills"}</button>
          <span class="t3" style="font-size:12px">checksum-checked, into every detected harness</span>
        </div>
        {out && <pre class="out" role="status" style="margin-top:12px">{out}</pre>}
      </div>
      {g && (
        <div>
          <div class="lbl">Guard</div>
          <div class="card">
            <div class="kv"><St s={g.enabled ? "ready" : "idle"} word={g.enabled ? "on" : "off"} /><span>Dangerous-command guard</span><span class="sp" /><span class="mono t3">{g.block.length} blocked · {g.allow.length} allowed</span></div>
            <div class="kv"><span style="width:52px" /><span>Fetch allowlist</span><span class="sp" /><span class="mono t3">{g.fetch_allow.length} hosts · {g.fetch_block.length} blocked</span></div>
            {!!missing.length && (
              <div class="kv" style="flex-wrap:wrap">
                <span class="t2">Suggested hosts not allowed yet: <span class="mono">{missing.slice(0, 4).map((h) => h.host).join(", ")}{missing.length > 4 ? ` and ${missing.length - 4} more` : ""}</span></span>
                <span class="sp" />
                <button class="btn sm" disabled={busy === "hosts"} onClick={addHosts}>Add {missing.length} suggested</button>
              </div>
            )}
          </div>
        </div>
      )}
      <More />
    </section>
  );
}
