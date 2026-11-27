import { useEffect, useState } from "preact/hooks";
import { Composer } from "./composer";
import { lazy, Suspense } from "preact/compat";
import { I } from "../../components/icons";
import { startSession } from "../../lib/live";
import { effortOptions } from "../../lib/parity";
import { addProject, loadProjects, startDir, type ProjectList } from "../../lib/projects";
import { go } from "../../lib/router";
import { choice, isDefault, type Ready, type Setup } from "../../lib/setup";
import { agentName, type Sess } from "../../lib/store";

const STARTERS: Record<string, string> = {
  smelt: "Fix the failing CI on this branch",
  scout: "Explain how this repo is laid out",
  tend: "Get my open PRs green",
  ledger: "Record the last decision as an ADR",
  warden: "Run the tasks in tasks/ in parallel",
};
const DIR = "smithy.dir";
const FolderPicker = lazy(() => import("../../components/folderpicker"));
const ModelSelect = lazy(() => import("./modelselect").then((m) => ({ default: m.ModelSelect })));

function remembered(): string {
  try {
    return localStorage.getItem(DIR) ?? "";
  } catch {
    return "";
  }
}

const modelOf = (r: Ready, s: Setup) => (!s.chosen || !isDefault(r, s.default) ? "" : r.kind === "api" ? s.default.agent.split("/").slice(1).join("/") : s.default.model ?? "");

// NewSession is where a session starts: the project it belongs to, the agent, its model and
// effort, and the first message.
const Splash = lazy(() => import("../../components/splash"));

export function NewSession({ setup, recent, host, shimmer, onAction }: { setup: Setup | null; recent: Sess[]; host: string; shimmer?: number; onAction: (a: string) => void }) {
  const [projects, setProjects] = useState<ProjectList | null>(null);
  const [perr, setPerr] = useState("");
  const [dir, setDir] = useState("");
  const [browse, setBrowse] = useState(false);
  const [pick, setPick] = useState("");
  const [model, setModel] = useState("");
  const [effort, setEffort] = useState("");
  const [draft, setDraft] = useState<string | undefined>();
  const ready = setup?.agents.filter((a) => a.ready) ?? [];
  const missing = (setup?.agents.length ?? 0) - ready.length;
  const cur = setup?.agents.find((a) => a.name === pick);
  const effs = cur ? effortOptions(cur.name) : [];

  useEffect(() => {
    loadProjects()
      .then((l) => {
        setProjects(l);
        setDir(startDir(l, remembered()));
      })
      .catch((e) => setPerr(String(e.message ?? e)));
  }, []);
  useEffect(() => {
    if (!setup || pick) return;
    const d = setup.agents.find((a) => a.ready && setup.chosen && isDefault(a, setup.default)) ?? ready[0];
    if (d) choose(d);
  }, [setup]);

  function choose(r: Ready) {
    if (!r.ready || !setup) return;
    setPick(r.name);
    setModel(modelOf(r, setup));
    setEffort("");
  }

  const add = async (d: string, def: boolean) => {
    const l = await addProject(d, def);
    setProjects(l);
    setDir(l.projects.find((p) => p.dir === d)?.dir ?? l.projects[l.projects.length - 1]?.dir ?? d);
    return "";
  };
  const makeDefault = async (d: string) => setProjects(await addProject(d, true));

  const send = async (text: string) => {
    if (!dir) return "Add a project first.";
    if (!cur) return "Pick an agent first.";
    const c = choice(cur, model);
    if (!c) return "An API agent needs a model.";
    const s = await startSession({ ...c, effort, dir, message: text });
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
  const pl = projects?.projects ?? [];
  return (
    <>
      <div class="scroll">
        <section class="hello ns" aria-label="New session">
          <header>
          {!recent.length && <Suspense fallback={null}><Splash /></Suspense>}
            <h2>New session</h2>
            <span class="tag"><I n="server" /><span>{host}</span></span>
          </header>

          <div>
            <div class="lbl">Project<span class="sp" /><button class="lnk" onClick={() => setBrowse(true)}><I n="plus" />Add project</button></div>
            <div class="card pick" role="radiogroup" aria-label="Project">
              {perr && <p class="err" style="padding:10px 12px">{perr}</p>}
              {!projects && !perr && <p class="hint">Reading projects…</p>}
              {projects && !pl.length && (
                <button class="row1 empty" onClick={() => setBrowse(true)}>
                  <I n="folder" /><span>No projects on {host} yet. Browse its folders to add one.</span>
                </button>
              )}
              {pl.map((p) => (
                <div class={"row1" + (dir === p.dir ? " on" : "")} role="radio" aria-checked={dir === p.dir} tabIndex={0}
                  onClick={() => setDir(p.dir)} onKeyDown={(e) => (e.key === " " || e.key === "Enter") && (e.preventDefault(), setDir(p.dir))}>
                  <span class="radio" aria-hidden="true" />
                  <b>{p.name}</b>
                  <span class="mono t3 path">{p.dir}</span>
                  <span class="sp" />
                  {projects?.default === p.dir ? <span class="t3 small">default</span> : (
                    <button class="lnk small" onClick={(e) => (e.stopPropagation(), makeDefault(p.dir))}>Set default</button>
                  )}
                </div>
              ))}
            </div>
          </div>

          <div>
            <div class="lbl">Agent<span class="sp" />{missing > 0 && <a class="t3" href="#/agents">{missing} not set up</a>}</div>
            <div class="card pick" role="radiogroup" aria-label="Agent">
              {!setup && <p class="hint">Reading the agents…</p>}
              {setup && !ready.length && <p class="hint">No agent is set up. <a href="#/agents">See how to add one</a>.</p>}
              {ready.map((r) => (
                <div class={"row1" + (pick === r.name ? " on" : "")} role="radio" aria-checked={pick === r.name} tabIndex={0}
                  onClick={() => choose(r)} onKeyDown={(e) => (e.key === " " || e.key === "Enter") && (e.preventDefault(), choose(r))}>
                  <span class="radio" aria-hidden="true" />
                  <b>{agentName(r.name)}</b>
                  <span class="t3 small">{r.kind === "api" ? "API" : "CLI"}</span>
                </div>
              ))}
            </div>
          </div>

          {cur && (
            <div class="opts">
              <div class="opt">
                <label class="lbl" for="model">Model</label>
                <Suspense fallback={null}><ModelSelect agent={cur} value={model} onChange={setModel} /></Suspense>
              </div>
              <div class="opt eff">
                <span class="lbl" id="effort-l">Effort</span>
                {effs.length ? (
                  <div class="seg" role="radiogroup" aria-labelledby="effort-l">
                    {effs.map((e) => (
                      <button role="radio" aria-checked={effort === e} class={effort === e ? "on" : ""} onClick={() => setEffort(e)}>{e || "default"}</button>
                    ))}
                  </div>
                ) : <span class="t3 small seg-none">{agentName(cur.name)} keeps its own</span>}
              </div>
            </div>
          )}

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
        paths={setup?.paths}
        agent={cur?.name ?? ""}
        shimmer={shimmer}
        draft={draft}
        canSend={!!cur && !!dir}
        onSend={send}
        onAction={onAction}
        pills={cur && (
          <span class="pill" title="The agent, model and effort above">
            <I n="term" />{agentName(cur.name)}<span class="hide-s t3">{[model || "default", effort].filter(Boolean).join(" · ")}</span>
          </span>
        )}
      />
      {browse && <Suspense fallback={null}><FolderPicker open={browse} host={host} start={dir ? dir.replace(/[\\/][^\\/]+$/, "") || "/" : ""} onPick={add} onClose={() => setBrowse(false)} /></Suspense>}
    </>
  );
}
