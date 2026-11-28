import { type ComponentType, render } from "preact";
import { lazy, Suspense } from "preact/compat";
import { useEffect, useMemo, useState } from "preact/hooks";
import "./theme/temper.css";
import "./app.css";
import { api, currentServer, initAuth } from "./lib/auth";
import { Composer } from "./features/chat/composer";
import { Conversation } from "./features/chat/conversation";
import { parse, totals, withComments, type Comment } from "./lib/diff";
import { Anvil, I } from "./components/icons";
import { sendMessage, start, stopSession, useLive } from "./lib/live";
import { clock, headerLine } from "./lib/parity";
import { go, href, PAGES, type Page, useView } from "./lib/router";
import { agentName, ago, open, sessions as list, type Sess } from "./lib/store";
import { initTheme, toggleTheme, current, loadAccent, type ThemeName } from "./lib/theme";
import { St } from "./components/ui";
import type { Info } from "./components/info";
import type { Detail } from "./lib/detail";

// The rail (diff viewer, decisions) loads after the first paint: it sits beside the chat.
const Changes = lazy(() => import("./components/rail").then((m) => ({ default: m.Changes })));
const Decisions = lazy(() => import("./components/rail").then((m) => ({ default: m.Decisions })));


const PAGE: Record<Page, ComponentType<{ onAction: (a: string) => void }>> = {
  decisions: lazy(() => import("./screens/decisions")),
  agents: lazy(() => import("./screens/agents")),
  board: lazy(() => import("./screens/board")),
  bench: lazy(() => import("./screens/bench")),
  workflows: lazy(() => import("./screens/workflows")),
  schedules: lazy(() => import("./screens/schedules")),
  setup: lazy(() => import("./screens/setup")),
};
const Palette = lazy(() => import("./components/palette"));
const AgentsPanel = lazy(() => import("./components/panel"));
const Splash = lazy(() => import("./components/splash"));
const SideServers = lazy(() => import("./components/servers").then((m) => ({ default: () => <m.Servers rows={m.useServers()} /> })));
const Refined = lazy(() => import("./features/chat/refined"));
const SessionPick = lazy(() => import("./features/chat/sessionpick"));
const InfoPanel = lazy(() => import("./components/info"));
const NewSession = lazy(() => import("./features/chat/newsession").then((m) => ({ default: m.NewSession })));

const ICON: Record<Page, string> = { decisions: "msg", agents: "bolt", board: "grid", bench: "chart", workflows: "layers", schedules: "clock", setup: "gear" };

type Auth = { state: "checking" } | { state: "ok" } | { state: "denied" } | { state: "down"; error: string };

function useAuth(): Auth {
  const [a, set] = useState<Auth>({ state: "checking" });
  useEffect(() => {
    api("/v1/state")
      .then((r) => set(r.ok ? { state: "ok" } : r.status === 401 ? { state: "denied" } : { state: "down", error: `HTTP ${r.status}` }))
      .catch((e) => set({ state: "down", error: String(e) }));
  }, []);
  return a;
}

function Gate({ auth }: { auth: Auth }) {
  if (auth.state === "checking") return null;
  return (
    <div class="gate" role="alert">
      <Suspense fallback={<Anvil />}><Splash /></Suspense>
      {auth.state === "denied" ? (
        <><h2>Not signed in</h2><p class="t2">Run <code>smithy web</code> and open the link it prints.</p></>
      ) : (
        <><h2>The daemon did not answer</h2><p class="t2">{auth.state === "down" && auth.error}. Is <code>smithy daemon</code> running?</p></>
      )}
    </div>
  );
}

const MACHINE = location.hostname === "localhost" || location.hostname === "127.0.0.1" || location.hostname === "[::1]" ? "This machine" : location.hostname;
const host = () => currentServer() || MACHINE;

function Sidebar({ ss, cur, pending, conn, sheet, onClose, onPalette, onCollapse }: {
  ss: Sess[]; cur: string; pending: number; conn: string; sheet: boolean; onClose: () => void; onPalette: () => void; onCollapse: () => void;
}) {
  const [mode, setMode] = useState(current);
  return (
    <aside class={"side" + (sheet ? " sheet" : "")} aria-label="Sessions" onClick={(e) => (e.target as HTMLElement).closest("a") && onClose()}>
      <div class="grab" />
      <div class="brand">
        <Anvil />smithy<span class="sp" />
        <button class="icon s" aria-label="Collapse sidebar" title="Collapse sidebar" onClick={sheet ? onClose : onCollapse}><I n="panel" /></button>
      </div>
      <a class="newbtn" href="#/new" aria-current={cur === "new" ? "page" : undefined}><I n="plus" />New session<kbd>⌥N</kbd></a>
      <div class="grp">
        <h4><I n="server" sm />{host()}<span class="sp" /><span class="mono">{ss.length}</span></h4>
        {!ss.length && <p class="muted-empty">No sessions yet.</p>}
        {ss.map((s) => (
          <a class="sess" href={href({ name: "session", id: s.id })} aria-current={cur === s.id ? "page" : undefined}>
            <span class="t">{s.title}</span>
            <span class="s"><St s={s.status} /><span>{s.status === "done" || s.status === "failed" ? ago(s.updated) : agentName(s.agent)}</span></span>
          </a>
        ))}
      </div>
      <Suspense fallback={null}><SideServers /></Suspense>
      <nav class="nav" aria-label="Pages">
        {PAGES.map((p) => (
          <a href={"#/" + p.id} aria-current={cur === p.id ? "page" : undefined} aria-keyshortcuts={"Alt+" + p.key}>
            <I n={ICON[p.id]} />{p.label}
            {p.id === "decisions" && pending > 0 && <span class="n">{pending}</span>}
          </a>
        ))}
      </nav>
      <div class="foot">
        <span class={"st conn " + (conn === "connected" ? "done" : "need")}>{conn === "connected" ? "live" : conn}</span>
        <span class="sp" />
        <button class="pill" onClick={onPalette} aria-label="Command palette" aria-keyshortcuts="Meta+K"><kbd>⌘K</kbd></button>
        <button class="icon" aria-label={mode === "dark" ? "Light mode" : "Dark mode"} title="Toggle theme" onClick={() => setMode(toggleTheme())}>
          <I n={mode === "dark" ? "sun" : "moon"} />
        </button>
      </div>
    </aside>
  );
}

function useClock(): string {
  const [t, set] = useState(() => clock(new Date()));
  useEffect(() => {
    const i = setInterval(() => set(clock(new Date())), 10000);
    return () => clearInterval(i);
  }, []);
  return t;
}

function App() {
  const now = useClock();
  const [version, setVersion] = useState("");
  const [pick, setPick] = useState(false);
  const [note, setNote] = useState("");
  const [shim, setShim] = useState(0);
  const v = useView();
  const auth = useAuth();
  const l = useLive();
  const [pal, setPal] = useState(false);
  const [accent, setAccent] = useState<ThemeName>("steel");
  const [palUsed, setPalUsed] = useState(false);
  const [sheet, setSheet] = useState<"" | "side" | "rail">("");
  const [collapsed, setCollapsed] = useState(false);
  const [notes, setNotes] = useState<Record<string, Comment[]>>({});
  const [info, setInfo] = useState<Info | null>(null);
  const [detail, setDetail] = useState<Detail>("expandable");
  const ss = list(l.store);
  const sid = v.name === "session" ? v.id : "";
  const s = sid ? l.store.sessions[sid] : undefined;
  const decs = useMemo(() => Object.fromEntries(l.store.decisions.map((d) => [d.id, d])), [l.store.decisions]);
  const pend = open(l.store);
  const diff = s ? l.store.diffs[s.id] : undefined;
  const t = useMemo(() => totals(parse(diff?.patch ?? "")), [diff?.patch]);
  const mine = notes[sid] ?? [];
  const skills = l.setup?.skills ?? [];
  const [project, setProject] = useState("");
  useEffect(() => {
    if (auth.state === "ok") api("/v1/projects").then((r) => (r.ok ? r.json() : null)).then((p) => p && setProject(p.default || p.projects?.[0]?.dir || "")).catch(() => {});
  }, [auth.state]);

  useEffect(() => {
    if (auth.state !== "ok") return;
    start();
    api("/v1/state").then((r) => (r.ok ? r.json() : null)).then((x) => x?.version && setVersion(x.version)).catch(() => {});
    loadAccent(api).then(setAccent).catch(() => {});
    if (!location.hash.replace(/^#\/?/, ""))
      api("/v1/setup").then((r) => (r.ok ? r.json() : null)).then((x) => x && !x.chosen && (location.hash = "#/setup")).catch(() => {});
  }, [auth.state]);
  useEffect(() => (setSheet(""), setNote(""), setPal(false)), [v]);

  const onAction = (a: string, arg = "") => {
    if (a === "new") go({ name: "new" });
    else import("./features/chat/slash").then((m) => m.run(a, arg, { s, pend, log: l.store.log, detail, setPick, setNote, setAccent, setDetail, setInfo, showTend: () => setSheet("rail"), shimmer: () => setShim((n) => n + 1) }));
  };

  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPalUsed(true);
        setPal((p) => !p);
      } else if (e.altKey && !e.ctrlKey && !e.metaKey) {
        if (e.code === "KeyN") {
          e.preventDefault();
          go({ name: "new" });
        }
        const p = PAGES.find((x) => "Digit" + x.key === e.code);
        if (p) {
          e.preventDefault();
          go({ name: p.id });
        }
      } else if (e.key === "Escape" && sheet) setSheet("");
      else if (e.key === "Enter" && document.activeElement === document.body) {
        const b = document.querySelector<HTMLButtonElement>(".conv .decide [data-primary]");
        if (b) {
          e.preventDefault();
          b.click();
        }
      }
    };
    addEventListener("keydown", on);
    return () => removeEventListener("keydown", on);
  }, [sheet]);

  if (auth.state !== "ok") return <Gate auth={auth} />;

  const title = v.name === "session" ? s?.title ?? "Session" : v.name === "new" ? "New session" : PAGES.find((p) => p.id === v.name)!.label;
  const rail = v.name !== "new" || !!ss.length;
  const railBody = s ? (
    <Suspense fallback={null}>
      <Changes diff={diff} comments={mine} onComment={(c) => setNotes({ ...notes, [sid]: [...mine, c] })} onDrop={(c) => setNotes({ ...notes, [sid]: mine.filter((x) => x !== c) })} />
      <Decisions list={l.store.decisions.filter((d) => d.session === s.id || (!d.session && d.answer == null))} sessions={l.store.sessions} />
      <AgentsPanel store={l.store} project={s.dir} cur={s.id} host={host()} />
    </Suspense>
  ) : (
    <Suspense fallback={null}><AgentsPanel store={l.store} project={project} cur="" host={host()} /></Suspense>
  );
  const panelBtn = (cls: string) =>
    !s && rail && (
      <button class={"icon s " + cls} onClick={() => setSheet("rail")} aria-label="Agents panel" title="Agents panel"><I n="bolt" /></button>
    );
  const delta = (cls: string) =>
    s && (
      <button class={"delta " + cls} onClick={() => setSheet("rail")} aria-label={`Changes: ${t.add} added, ${t.del} removed`}>
        <span class="plus">+{t.add}</span><span class="minus">−{t.del}</span>
      </button>
    );

  let center;
  if (v.name === "new") center = <Suspense fallback={null}><NewSession setup={l.setup} recent={ss} host={host()} shimmer={shim} onAction={onAction} /></Suspense>;
  else if (v.name === "session")
    center = s ? (
      <>
        <Conversation s={s} detail={detail} items={l.store.convo[s.id] ?? []} decisions={decs} loose={s.status === "working" || s.status === "needs_you" ? pend.filter((d) => !d.session) : []}
          extra={[<Suspense fallback={null}><Refined s={s} items={l.store.convo[s.id]} /></Suspense>]} />
        <Composer
          placeholder={`Reply to ${agentName(s.agent)}`}
          skills={skills}
          paths={l.setup?.paths}
          agent={s.agent}
          shimmer={shim}
          working={s.status === "working" || s.status === "needs_you"}
          onStop={() => stopSession(s.id).catch(() => {})}
          onAction={onAction}
          onShell={currentServer() ? undefined : async (cmd) => {
            await (await import("./lib/actions")).runShell(s.id, cmd);
          }}
          onImage={currentServer() ? undefined : async (f) => (await import("./lib/actions")).uploadImage(f)}
          above={!!mine.length && <div class="cmts"><span class="tag"><I n="pen" />{mine.length} diff comment{mine.length > 1 ? "s" : ""} go with this message</span></div>}
          onSend={async (text) => {
            await sendMessage(s.id, withComments(text, mine));
            setNotes({ ...notes, [sid]: [] });
            return "";
          }}
          pills={
            <>
              <button class="pill" aria-haspopup="dialog" title="Model and effort (/model, /effort)" onClick={() => setPick(true)}>
                <I n="term" />{agentName(s.agent)}<span class="hide-s t3">{(s.model || "default") + (s.effort ? " · " + s.effort : "")}</span>
              </button>
              {s.goal && <span class="pill" title={s.goal}>goal {s.goal_state || "on"}</span>}
            </>
          }
        />
      </>
    ) : (
      <div class="empty"><h2>{l.store.last ? "No such session" : "Loading…"}</h2>{l.store.last ? <a class="btn" href="#/new">New session</a> : null}</div>
    );
  else {
    const P = PAGE[v.name];
    center = <div class="scroll"><Suspense fallback={null}><P onAction={onAction} /></Suspense></div>;
  }

  return (
    <div class={"app" + (rail ? " with-rail" : "") + (collapsed ? " collapsed" : "")}>
      <a class="skip" href="#main" onClick={(e) => (e.preventDefault(), document.getElementById("main")?.focus())}>Skip to content</a>
      <header class="top">
        <button class="icon" aria-label="Sessions" onClick={() => setSheet("side")}><I n="menu" /></button>
        <h1>{title}</h1>
        {s && <St s={s.status} />}
        {delta("")}
        {panelBtn("")}
      </header>
      {sheet === "side" && <div class="scrim for-side" onClick={() => setSheet("")} />}
      <Sidebar ss={ss} cur={v.name === "session" ? v.id : v.name} pending={pend.length} conn={l.conn} sheet={sheet === "side"} onClose={() => setSheet("")}
        onPalette={() => (setPalUsed(true), setPal(true))} onCollapse={() => setCollapsed(true)} />
      <main class="main" id="main" tabIndex={-1}>
        <div class="meta-bar">
          {collapsed && <button class="icon s" aria-label="Show sidebar" onClick={() => setCollapsed(false)}><I n="panel" /></button>}
          <span class="brandline mono" title={s?.dir}>{headerLine(version, s?.dir ?? "")}</span>
          <h1>{title}</h1>
          {s ? <St s={s.status} /> : <span class={"st " + (l.conn === "connected" ? "done" : "need")}>{l.conn === "connected" ? "live" : l.conn}</span>}
          {note && <span class="t3 note" role="status" onClick={() => setNote("")}>{note}</span>}
          <span class="sp" />
          {s && <span class="tag" title={"Runs on " + host()}><I n="server" sm /><span class="mono">{host()}</span></span>}
          {delta("rail-btn")}
          {panelBtn("rail-btn")}
          {s && (s.status === "working" || s.status === "needs_you") && (
            <button class="btn sm" onClick={() => stopSession(s.id).catch(() => {})}>Stop<kbd>esc</kbd></button>
          )}
          <span class="mono t3 clock" aria-label="Time">{now}</span>
        </div>
        {center}
      </main>
      {rail && sheet !== "rail" && <aside class="rail" aria-label="Changes and decisions">{railBody}</aside>}
      {sheet === "rail" && (
        <>
          <div class="scrim" onClick={() => setSheet("")} />
          <aside class="rail sheet" aria-label="Changes and decisions"><div class="grab" />{railBody}</aside>
        </>
      )}
      {pick && s && (
        <Suspense fallback={null}>
          <SessionPick s={s} open={pick} onClose={() => setPick(false)} onTop={() => setShim((n) => n + 1)} />
        </Suspense>
      )}
      {info && (
        <Suspense fallback={null}>
          <InfoPanel info={info} onClose={() => setInfo(null)} />
        </Suspense>
      )}
      {palUsed && (
        <Suspense fallback={null}>
          <Palette open={pal} onOpenChange={setPal} onAction={onAction} theme={accent} />
        </Suspense>
      )}
    </div>
  );
}

initTheme();
initAuth();
render(<App />, document.getElementById("app")!);

if (import.meta.env.PROD && "serviceWorker" in navigator) navigator.serviceWorker.register("/sw.js").catch(() => {});
