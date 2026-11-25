import { useEffect, useState } from "preact/hooks";
import { Composer } from "./composer";
import { I } from "./icons";
import { startSession } from "./live";
import { go } from "./router";
import { choice, isDefault, type Ready, type Setup } from "./setup";
import { agentName, type Sess } from "./store";
import { St } from "./ui";

const STARTERS: Record<string, string> = {
  smelt: "Fix the failing CI on this branch",
  scout: "Explain how this repo is laid out",
  tend: "Get my open PRs green",
  ledger: "Record the last decision as an ADR",
  warden: "Run the tasks in tasks/ in parallel",
};
const DIR = "smithy.dir";

function remembered(): string {
  try {
    return localStorage.getItem(DIR) ?? "";
  } catch {
    return "";
  }
}

const modelOf = (r: Ready, s: Setup) => (!s.chosen || !isDefault(r, s.default) ? "" : r.kind === "api" ? s.default.agent.split("/").slice(1).join("/") : s.default.model ?? "");

// NewSession is where a session starts: where it runs (server, folder), which agent and model,
// and a few skill starters. The first message starts it.
export function NewSession({ setup, recent, host, onAction }: { setup: Setup | null; recent: Sess[]; host: string; onAction: (a: string) => void }) {
  const [dir, setDir] = useState(() => remembered() || recent[0]?.dir || "");
  const [pick, setPick] = useState("");
  const [model, setModel] = useState("");
  const [draft, setDraft] = useState<string | undefined>();
  const [all, setAll] = useState(false);
  const ready = setup?.agents.filter((a) => a.ready) ?? [];
  const cur = setup?.agents.find((a) => a.name === pick);

  useEffect(() => {
    if (!setup || pick) return;
    const d = setup.agents.find((a) => a.ready && setup.chosen && isDefault(a, setup.default)) ?? ready[0];
    if (d) {
      setPick(d.name);
      setModel(modelOf(d, setup));
    }
  }, [setup]);
  useEffect(() => {
    if (!dir && recent[0]?.dir) setDir(recent[0].dir);
  }, [recent[0]?.dir]);

  const choose = (r: Ready) => {
    if (!r.ready || !setup) return;
    setPick(r.name);
    setModel(modelOf(r, setup));
  };

  const send = async (text: string) => {
    if (!cur) return "Pick an agent first.";
    const c = choice(cur, model);
    if (!c) return "An API agent needs a model.";
    if (!dir.startsWith("/") && !/^[A-Za-z]:\\/.test(dir)) return "The folder must be an absolute path, like /Users/you/Codes/api.";
    const s = await startSession({ ...c, dir, message: text });
    try {
      localStorage.setItem(DIR, dir);
    } catch {
      /* not remembered */
    }
    go({ name: "session", id: s.id });
    return "";
  };

  const skills = setup?.skills ?? [];
  const starters = skills.filter((k) => STARTERS[k]);
  const ag = cur ? agentName(cur.name) : "an agent";
  return (
    <>
      <div class="scroll">
        <section class="hello" aria-label="New session">
          <div>
            <h2>New session</h2>
            <div class="ctx">
              <span class="tag"><I n="server" />{host}</span>
              <label class="tag" title="The folder the agent works in">
                <I n="folder" />
                <input value={dir} onInput={(e) => setDir(e.currentTarget.value)} placeholder="/path/to/repo" aria-label="Folder" spellcheck={false} autoComplete="off" />
              </label>
            </div>
          </div>
          <div>
            <div class="lbl">Agents on {host}<span class="sp" /><a class="t3" href="#/setup">Settings</a></div>
            <div class="card agents" role="radiogroup" aria-label="Agent">
              {!setup && <p class="hint" style="padding:12px">Reading the agents…</p>}
              {[...ready, ...(all ? setup?.agents.filter((a) => !a.ready) ?? [] : [])].map((r) => (
                <>
                  <div
                    class={"row2" + (r.ready ? "" : " off") + (pick === r.name ? " on" : "")}
                    role="radio"
                    aria-checked={pick === r.name}
                    aria-disabled={!r.ready}
                    tabIndex={r.ready ? 0 : -1}
                    onClick={() => choose(r)}
                    onKeyDown={(e) => (e.key === " " || e.key === "Enter") && (e.preventDefault(), choose(r))}
                  >
                    {r.ready ? <span class="radio" aria-hidden="true" /> : <span style="width:16px" />}
                    <b>{agentName(r.name)}</b>
                    <span class="mono">{r.ready ? (r.kind === "api" ? r.detail : r.detail.split("/").pop()) : r.fix || r.detail}</span>
                    <span class="sp" />
                    <St s={r.ready ? "ready" : "missing"} />
                  </div>
                  {pick === r.name && (
                    <div class="model">
                      <label class="t3" for="model" style="font-size:12px">Model</label>
                      <input id="model" value={model} onInput={(e) => setModel(e.currentTarget.value)} placeholder={r.kind === "api" ? "required, e.g. claude-sonnet-5-5" : "the agent's default"} spellcheck={false} autoComplete="off" />
                    </div>
                  )}
                </>
              ))}
              {setup && setup.agents.length > ready.length && (
                <button class="row2 t3" style="flex-direction:row;font-size:12px" aria-expanded={all} onClick={() => setAll(!all)}>
                  {all ? "Hide the ones not set up" : `${setup.agents.length - ready.length} more not set up`}
                </button>
              )}
            </div>
          </div>
          {!!starters.length && (
            <div>
              <div class="lbl">Start from a skill</div>
              <div class="card starts">
                {starters.map((k) => (
                  <button class="row2" onClick={() => setDraft(`/${k} ${STARTERS[k]}`)}>
                    <span>{STARTERS[k]}</span>
                    <span class="sp" />
                    <span class="mono t3">/{k}</span>
                  </button>
                ))}
              </div>
            </div>
          )}
        </section>
      </div>
      <Composer
        placeholder={`Ask ${ag} to…`}
        skills={skills}
        draft={draft}
        canSend={!!cur}
        onSend={send}
        onAction={onAction}
        pills={cur && (
          <span class="pill" title="Pick the agent above">
            <I n="term" />{agentName(cur.name)}<span class="hide-s t3">{model || "default"}</span>
          </span>
        )}
      />
    </>
  );
}
