// The smithy commands the composer and the palette run here, loaded on first use: the TUI's
// /model, /effort, /goal, /theme, /mode, /ask, /answer, /serve, /set, /unset, /key rm, the panels (/help,
// /log, /remote, /tend, /links, /sidebar) and the ones that open Settings.
import { addLink, removeKey, runShell, sampleAsk, setServe, setSessionGoal, setSessionModel, setSetting, unsetSetting } from "../../lib/actions";
import { ACTIONS } from "../../lib/commands";
import { nextDetail, type Detail } from "../../lib/detail";
import type { Info } from "../../components/info";
import { api } from "../../lib/auth";
import { decide, stopSession } from "../../lib/live";
import { effortLevels, effortMax, effortOptions } from "../../lib/parity";
import { go } from "../../lib/router";
import type { Dec, Sess } from "../../lib/store";
import { current, saveAccent, toggleTheme, type ThemeName } from "../../lib/theme";

type Ctx = {
  s?: Sess; pend: Dec[]; log: string[]; detail: Detail;
  setPick: (o: boolean) => void; setNote: (n: string) => void; setAccent: (t: ThemeName) => void;
  setDetail: (d: Detail) => void; setInfo: (i: Info) => void; showTend: () => void; shimmer: () => void;
};

const help = (): Info => ({
  title: "Help",
  lines: ["#KEYS", "⌘K\tcommand palette", "⌥N\tnew session", "/\tcommands and skills", "!cmd\trun a shell command in the session's folder", "esc\tstop the turn",
    "#COMMANDS", ...ACTIONS.map((a) => "/" + a.name + "\t" + a.hint)],
});

// remote is /remote as the TUI shows it, for the address this page was opened on.
function remote(): Info {
  const port = location.port || "7477";
  const lines = ["#FROM ANOTHER MACHINE"];
  if (location.hostname.endsWith(".ts.net")) lines.push("ok " + location.origin + "\tserved on your tailnet; daemon.allow decides who gets in");
  else lines.push("-> tailscale serve --bg " + port + "\tserves this daemon on your tailnet, nothing exposed to the internet",
    "-> smithy config set daemon.allow <login>\tthe tailnet logins let in, comma separated",
    "then open https://<this machine>.<tailnet>.ts.net\ton your phone, or smithy attach it from a laptop");
  return { title: "Remote", lines };
}

// effort is /effort as the TUI runs it: a typed level sets the open session's effort, or without a
// session agent.effort, the default the next session starts with; off and default put it back.
// The top level plays the composer shimmer.
function effort(arg: string, { s, setPick, setNote, shimmer }: Ctx) {
  const typed = arg.toLowerCase();
  const lvl = typed === "off" || typed === "default" ? "" : typed;
  const top = () => lvl === effortMax() && shimmer();
  const fail = (e: unknown) => setNote("error: " + String((e as Error).message ?? e));
  if (s) {
    const opts = effortOptions(s.agent);
    if (!opts.length) setNote(s.agent + " keeps its own effort; /effort works for claude and codex");
    else if (!arg) setPick(true);
    else if (!opts.includes(lvl)) setNote("effort must be off or one of " + opts.slice(1).join(", "));
    else setSessionModel(s.id, s.model ?? "", lvl).then(() => (setNote("next turns run effort " + (lvl || "agent.effort")), top()), fail);
    return;
  }
  const opts = effortLevels();
  if (!arg) toSettings("agents");
  else if (!opts.includes(lvl)) setNote("effort must be off or one of " + opts.slice(1).join(", ") + " (smithy caps effort at " + effortMax() + ")");
  else if (!lvl) unsetSetting("agent.effort").then(() => setNote("agent.effort back to its default (" + effortMax() + "): the next session starts with it"), fail);
  else setSetting("agent.effort", lvl).then(() => (setNote("saved agent.effort = " + lvl + ": the next session starts with it"), top()), fail);
}

const goalLabel = (state?: string): string =>
  state === "met" ? " met" : state === "asked" ? " paused: the agent asks you" : state === "capped" ? " paused: turn or cost cap reached" : "";

// goal is /goal as the TUI runs it: no text shows the open session's goal, clear drops it, any
// other text pins it; the daemon then continues the session until it is met or a cap stops it.
function goal(arg: string, { s, setNote }: Ctx) {
  if (!s) return setNote("open a session first: /goal pins a goal on the open session");
  if (!arg) return setNote(s.goal ? "goal" + goalLabel(s.goal_state) + ": " + s.goal : "no goal on this session; /goal <text> pins one");
  const g = arg === "clear" ? "" : arg;
  setSessionGoal(s.id, g).then(() => setNote(g ? "goal set: " + g : "goal cleared"), (e) => setNote("error: " + String((e as Error).message ?? e)));
}

const toSettings = (part: string) => {
  go({ name: "setup" });
  setTimeout(() => document.getElementById("set-" + part)?.scrollIntoView({ block: "start" }), 300);
};

export function run(a: string, arg: string, ctx: Ctx): void {
  const { s, pend, log, detail, setPick, setNote, setAccent, setDetail, setInfo, showTend } = ctx;
  const say = (p: Promise<unknown>, ok: string) => p.then(() => setNote(ok), (e) => setNote("error: " + String((e as Error).message ?? e)));
  if (a.startsWith("theme:")) {
    arg = a.slice(6);
    a = "theme";
  }
  if (a === "stop" && s) stopSession(s.id).catch(() => {});
  else if (a === "effort") effort(arg, ctx);
  else if (a === "goal") goal(arg, ctx);
  else if (a === "model") {
    if (s) setPick(true);
    else toSettings("agents");
  } else if (a === "theme" && (arg === "light" || arg === "dark" || arg === "toggle")) {
    if (arg !== current()) toggleTheme();
    setNote(current() + " mode");
  } else if (a === "theme" && (arg === "steel" || arg === "frost" || arg === "plum")) {
    setAccent(arg);
    say(saveAccent(api, arg), "theme " + arg);
  } else if (a === "theme") toSettings("theme");
  else if (a === "mode") {
    const d = nextDetail(detail, arg);
    setDetail(d);
    setNote("transcript: " + d);
  } else if (a === "help") setInfo(help());
  else if (a === "remote") setInfo(remote());
  else if (a === "log") setInfo({ title: "Log", lines: ["#DAEMON LOG · " + log.length + " EVENTS", ...(log.length ? log : ["nothing yet"])] });
  else if (a === "links" && arg && s) say(addLink(s.id, arg.trim()), "linked " + arg.trim());
  else if (a === "sidebar") {
    if (document.querySelector<HTMLElement>(".rail:not(.sheet)")?.offsetParent) setNote("the side panel is beside the chat");
    else showTend();
  } else if (a === "tend" || a === "links") {
    const side = document.querySelector<HTMLElement>(".rail:not(.sheet) #" + a);
    if (side?.offsetParent) side.scrollIntoView({ block: "start" });
    else {
      showTend();
      setTimeout(() => document.querySelector(".rail.sheet #" + a)?.scrollIntoView({ block: "start" }), 300);
    }
  }
  else if (a === "key" && /^rm\s+\S+$/.test(arg)) {
    const svc = arg.split(/\s+/)[1];
    say(removeKey(svc), "removed the " + svc + " key");
  } else if (a === "key") toSettings("keys");
  else if (a === "ask") say(sampleAsk(arg), "a sample question is pinned: answer it here or in Decisions");
  else if (a === "answer") {
    const d = pend.find((x) => !s || x.session === s.id || !x.session);
    if (!d || !arg) setNote(d ? "usage: /answer <text>" : "no open decision");
    else say(decide(d.id, arg), "answered: " + arg);
  } else if (a === "serve" && (arg === "on" || arg === "off")) say(setServe(arg === "on"), "serve " + arg);
  else if (a === "set" && arg.includes(" ")) {
    const [k, ...v] = arg.split(/\s+/);
    say(setSetting(k, v.join(" ")), "saved " + k);
  } else if (a === "unset" && arg) say(unsetSetting(arg), arg + " is back to its default");
  else if (a === "set" || a === "unset") toSettings("settings");
  else if (a === "skills" || a === "keys" || a === "doctor" || a === "serve" || a === "prompt" || a === "channels") toSettings(a);
  else if (a === "connect") toSettings("servers");
  else if (a === "agent" || a === "setup") toSettings("agents");
  else if (a === "welcome") go({ name: "setup" });
  else if (a === "sessions") go({ name: "agents" });
  else if (a === "settings") go({ name: "setup" });
  else if (a === "agents" || a === "board" || a === "decisions" || a === "bench" || a === "workflows" || a === "schedules") go({ name: a });
  else if (a === "run") go({ name: "workflows" });
}

// shell is `!cmd`: the last 200 lines, like the TUI.
export async function shell(id: string, cmd: string): Promise<{ cmd: string; output: string; code: number }> {
  const r = await runShell(id, cmd);
  const out = r.output.split("\n");
  const output = (out.length > 200 ? `… ${out.length - 200} lines cut\n` + out.slice(-200).join("\n") : r.output) + (r.error ? "\n" + r.error : "");
  return { cmd, output, code: r.code };
}
