// The smithy commands the composer and the palette run here, loaded on first use: the TUI's
// /model, /effort, /theme, /ask, /answer, /serve, /set, /unset and the ones that open Settings.
import { runShell, sampleAsk, setServe, setSetting, unsetSetting } from "../../lib/actions";
import { api } from "../../lib/auth";
import { decide, stopSession } from "../../lib/live";
import { go } from "../../lib/router";
import type { Dec, Sess } from "../../lib/store";
import { saveAccent, toggleTheme, type ThemeName } from "../../lib/theme";

type Ctx = { s?: Sess; pend: Dec[]; setPick: (o: boolean) => void; setNote: (n: string) => void; setAccent: (t: ThemeName) => void };

const toSettings = (part: string) => {
  go({ name: "setup" });
  setTimeout(() => document.getElementById("set-" + part)?.scrollIntoView({ block: "start" }), 300);
};

export function run(a: string, arg: string, { s, pend, setPick, setNote, setAccent }: Ctx): void {
  const say = (p: Promise<unknown>, ok: string) => p.then(() => setNote(ok), (e) => setNote("error: " + String((e as Error).message ?? e)));
  if (a.startsWith("theme:")) {
    arg = a.slice(6);
    a = "theme";
  }
  if (a === "stop" && s) stopSession(s.id).catch(() => {});
  else if (a === "model" || a === "effort") {
    if (s) setPick(true);
    else toSettings("agents");
  } else if (a === "theme" && (arg === "steel" || arg === "frost" || arg === "plum")) {
    setAccent(arg);
    say(saveAccent(api, arg), "theme " + arg);
  } else if (a === "theme") toSettings("theme");
  else if (a === "mode") toggleTheme();
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
