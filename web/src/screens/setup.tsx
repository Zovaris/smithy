import { useEffect, useState } from "preact/hooks";
import { Button } from "@zovaris/sephiro/button";
import "@zovaris/sephiro/css/button.css";
import { G } from "../ui";
import { choice, installSkills, isDefault, loadSetup, type Ready, setDefault, setKit, type Setup as S, toggleSkill } from "../setup";
import "./chat.css";
import "./setup.css";

function AgentRow({ r, s, open, onOpen, onSaved }: { r: Ready; s: S; open: boolean; onOpen: () => void; onSaved: () => void }) {
  const cur = s.chosen && isDefault(r, s.default);
  const [model, setModel] = useState(cur && r.kind === "cli" ? s.default.model ?? "" : "");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const use = async (e: Event) => {
    e.preventDefault();
    const c = choice(r, model);
    if (!c) return setErr("an API agent needs a model");
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
    <li class={"arow" + (open ? " open" : "") + (cur ? " cur" : "")}>
      <button class="ahead" aria-expanded={r.ready ? open : undefined} onClick={onOpen} disabled={!r.ready}>
        <G s={r.ready ? "ok" : "failed"} />
        <span class="strong aname">{r.name}</span>
        <span class="dim astate">{r.ready ? "ready" : "missing"}</span>
        {r.ready ? <span class="dim adetail">{r.detail}</span> : <code class="afix">{r.fix}</code>}
        {cur && <span class="adef">default</span>}
      </button>
      {open && r.ready && (
        <form class="apick" onSubmit={use}>
          <label>
            <span class="dim">{r.kind === "api" ? "Model (required)" : "Model (empty: the agent's default)"}</span>
            <input value={model} onInput={(e) => setModel((e.target as HTMLInputElement).value)} placeholder={r.kind === "api" ? "claude-sonnet-5-5" : "opus, gpt-5…"} spellcheck={false} autoComplete="off" />
          </label>
          <Button size="sm" type="submit" loading={busy}>Use {r.name}</Button>
          {err && <span class="g failed" role="alert">✗ {err}</span>}
        </form>
      )}
    </li>
  );
}

// Setup is first run and the agent/model picker (like ctrl+l in the TUI), then what goes into
// every session (smithy's MCP server, smithy skills) and what each harness has installed.
export default function Setup() {
  const [s, setS] = useState<S | null>(null);
  const [err, setErr] = useState("");
  const [open, setOpen] = useState("");
  const [out, setOut] = useState("");
  const [installing, setInstalling] = useState(false);
  const load = () => loadSetup().then(setS, (e) => setErr(String(e.message ?? e)));
  useEffect(() => void load(), []);
  if (err && !s) return <p class="msg" role="alert"><G s="failed" />could not read the agents: {err}</p>;
  if (!s) return <p class="dim">reading the agents…</p>;
  const kit = async (k: S["kit"]) => {
    setS({ ...s, kit: k });
    try {
      await setKit(k);
    } catch (e) {
      setErr(String((e as Error).message ?? e));
    }
    load();
  };
  const install = async () => {
    setInstalling(true);
    setOut("installing smithy skills into every detected harness…");
    try {
      setOut((await installSkills()).output.trim() || "done");
    } catch (e) {
      setOut("install failed: " + String((e as Error).message ?? e));
    }
    setInstalling(false);
    load();
  };
  const cli = s.agents.filter((a) => a.kind === "cli");
  const apis = s.agents.filter((a) => a.kind === "api");
  const ready = s.agents.filter((a) => a.ready).length;
  const rows = (list: Ready[]) => (
    <ul class="list agents">
      {list.map((r) => (
        <AgentRow key={r.name} r={r} s={s} open={open === r.name} onOpen={() => setOpen(open === r.name ? "" : r.name)} onSaved={() => { setOpen(""); load(); }} />
      ))}
    </ul>
  );
  return (
    <section aria-label="Setup" class="setup">
      {s.chosen ? (
        <h1 class="h">Setup <span class="dim">{ready} of {s.agents.length} agents ready · new sessions start with {s.default.agent}{s.default.model ? " · " + s.default.model : ""}</span></h1>
      ) : (
        <>
          <h1 class="h">Welcome to smithy</h1>
          <p class="lead">These are the agents on this machine. Pick the one new sessions start with; you can change it here or with <kbd>ctrl+l</kbd> in the TUI.</p>
        </>
      )}
      {err && <p class="g failed" role="alert">✗ {err}</p>}
      <h2 class="h2 muted">Agent CLIs</h2>
      {rows(cli)}
      <h2 class="h2 muted">API keys</h2>
      {rows(apis)}

      <h2 class="h2 muted">Injected into every session</h2>
      <ul class="list kit">
        <li>
          <label class="krow">
            <input type="checkbox" checked={s.kit.mcp} onChange={() => kit({ ...s.kit, mcp: !s.kit.mcp })} />
            <span class="strong kname">smithy MCP server</span>
            <span class="dim">board, cards and decisions as tools</span>
          </label>
        </li>
        {s.skills.map((k) => (
          <li>
            <label class="krow">
              <input type="checkbox" checked={s.kit.skills.includes(k)} onChange={() => kit(toggleSkill(s.kit, k))} />
              <span class="strong kname">{k}</span>
              <span class="dim">skill</span>
            </label>
          </li>
        ))}
      </ul>
      {!s.skills.length && <p class="dim">no smithy skills installed yet</p>}

      <h2 class="h2 muted">Installed per harness</h2>
      <div class="tablewrap">
        <table class="harn">
          <thead><tr><th>harness</th><th>smithy skills</th><th>other skills</th><th>MCP servers</th></tr></thead>
          <tbody>
            {s.harnesses.map((h) => (
              <tr class={h.present ? "" : "absent"}>
                <td>{h.present ? <G s={h.missing.length ? "waiting" : "ok"} /> : <span class="g dim" aria-label="not installed">·</span>} <span class={h.present ? "strong" : "dim"}>{h.harness}</span></td>
                {h.present ? (
                  <>
                    <td title={h.missing.length ? "missing: " + h.missing.join(", ") : ""}>{h.smithy.length}/{h.smithy.length + h.missing.length}</td>
                    <td>{h.other.length}</td>
                    <td class={h.mcp_error ? "fail" : ""}>{h.mcp_error ?? (h.mcp.join(", ") || <span class="dim">none</span>)}</td>
                  </>
                ) : (
                  <td colSpan={3} class="dim">not installed</td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p class="inst">
        <Button size="sm" variant="quiet" loading={installing} onClick={install}>Install / update smithy skills</Button>
        <span class="dim">checksum-checked, into every detected harness</span>
      </p>
      {out && <pre class="out instout" role="status">{out}</pre>}
    </section>
  );
}
