// The smithy commands the composer and the palette run here, loaded on first use: the TUI's
// /model, /effort, /theme, /mode, /ask, /answer, /serve, /set, /unset, /key rm, the panels (/help,
// /log, /remote, /tend) and the ones that open Settings.
import { removeKey, runShell, sampleAsk, setServe, setSetting, unsetSetting } from "../../lib/actions";
import { ACTIONS, nextDetail, type Detail } from "../../lib/commands";
import type { Info } from "../../components/info";
import { api } from "../../lib/auth";
import { decide, stopSession } from "../../lib/live";
import { go } from "../../lib/router";
import type { Dec, Sess } from "../../lib/store";
import { current, saveAccent, toggleTheme, type ThemeName } from "../../lib/theme";

type Ctx = {
  s?: Sess; pend: Dec[]; log: string[]; detail: Detail;
  setPick: (o: boolean) => void; setNote: (n: string) => void; setAccent: (t: ThemeName) => void;
  setDetail: (d: Detail) => void; setInfo: (i: Info) => void; showTend: () => void;
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

const toSettings = (part: string) => {
  go({ name: "setup" });
  setTimeout(() => document.getElementById("set-" + part)?.scrollIntoView({ block: "start" }), 300);
};

export function run(a: string, arg: string, { s, pend, log, detail, setPick, setNote, setAccent, setDetail, setInfo, showTend }: Ctx): void {
  const say = (p: Promise<unknown>, ok: string) => p.then(() => setNote(ok), (e) => setNote("error: " + String((e as Error).message ?? e)));
  if (a.startsWith("theme:")) {
    arg = a.slice(6);
    a = "theme";
  }
  if (a === "stop" && s) stopSession(s.id).catch(() => {});
  else if (a === "model" || a === "effort") {
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
  else if (a === "tend") showTend();
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
  else if (a === "skills" || a === "keys" || a === "doctor" || a === "serve") toSettings(a);
  else if (a === "connect") toSettings("servers");
  else if (a === "agent" || a === "setup") toSettings("agents");
  else if (a === "welcome") go({ name: "setup" });
  else if (a === "sessions") go({ name: "agents" });
  else if (a === "settings") go({ name: "setup" });
  else if (a === "agents" || a === "board" || a === "decisions" || a === "bench") go({ name: a });
}

// shell is `!cmd`: the last 200 lines, like the TUI.
export async function shell(id: string, cmd: string): Promise<{ cmd: string; output: string; code: number }> {
  const r = await runShell(id, cmd);
  const out = r.output.split("\n");
  const output = (out.length > 200 ? `… ${out.length - 200} lines cut\n` + out.slice(-200).join("\n") : r.output) + (r.error ? "\n" + r.error : "");
  return { cmd, output, code: r.code };
}
