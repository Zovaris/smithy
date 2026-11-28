import { describe, expect, it } from "vitest";
import popup from "../../../internal/tui/chat/chat_popup.go?raw";
import slash from "../features/chat/slash.ts?raw";
import { ACTIONS, action, suggest } from "./commands";
import { nextDetail } from "./detail";
import { THEMES } from "./theme";

// The TUI commands the web leaves out on purpose: it has no terminal to detach from or quit, and
// it already is the web UI.
const TUI_ONLY = ["detach", "quit", "web"];

// The web commands the TUI has no command for: each opens a web-only page.
const WEB_ONLY = ["agents", "decisions", "bench", "settings"];

const names = ACTIONS.map((a) => a.name);

describe("commands", () => {
  it("suggests actions and skills for a bare /word, prefix matches first", () => {
    expect(suggest("/st", ["smelt", "tend"]).map((c) => c.name)).toEqual(["stop"]);
    expect(suggest("/se", ["smelt", "scout"]).map((c) => c.name)).toEqual(["sessions", "setup", "serve", "set", "settings", "unset"]);
    expect(suggest("/", []).length).toBe(ACTIONS.length);
  });
  it("lists the commands first, then the skills marked as skills", () => {
    const l = suggest("/s", ["scout", "smelt"]);
    expect(l.slice(-2).map((c) => [c.name, c.kind])).toEqual([["scout", "skill"], ["smelt", "skill"]]);
    expect(l.slice(0, -2).every((c) => c.kind === "action")).toBe(true);
    expect(suggest("/te", ["tester"]).map((c) => c.name)).toEqual(["tend", "remote", "tester"]);
  });
  it("matches a description from three letters on, and shows where a skill lives", () => {
    expect(suggest("/daemon", []).map((c) => c.name)).toEqual(["log"]);
    expect(suggest("/da", []).map((c) => c.name)).toEqual([]);
    const paths = { smelt: "/Users/me/.claude/skills/smelt/SKILL.md", scout: "/opt/kit/scout/SKILL.md" };
    expect(suggest("/smelt", ["smelt"], paths)[0]).toEqual({ name: "smelt", hint: "~/.claude/skills", kind: "skill" });
    expect(suggest("/kit", ["smelt", "scout"], paths).map((c) => c.name)).toEqual(["scout"]);
  });
  it("stops suggesting once the message goes on", () => {
    expect(suggest("/smelt fix", ["smelt"])).toEqual([]);
    expect(suggest("hello", ["smelt"])).toEqual([]);
  });
  it("runs only a message that is one smithy command", () => {
    expect(action(" /stop ")).toBe("stop");
    expect(action("/smelt fix ci")).toBe("");
    expect(action("/smelt")).toBe("");
    expect(action("stop")).toBe("");
  });

  it("has every TUI chat command except the ones that only make sense in a terminal", () => {
    const tui = [...popup.matchAll(/^\t\{"(\w+)",/gm)].map((m) => m[1]);
    expect(tui.length).toBeGreaterThan(20);
    expect(tui.filter((c) => !TUI_ONLY.includes(c) && !names.includes(c))).toEqual([]);
    expect(names.filter((c) => TUI_ONLY.includes(c))).toEqual([]);
  });
  it("has no command the TUI lacks unless it is marked web-only", () => {
    const tui = [...popup.matchAll(/^\t\{"(\w+)",/gm)].map((m) => m[1]);
    expect(names.filter((c) => !tui.includes(c) && !WEB_ONLY.includes(c))).toEqual([]);
    expect(WEB_ONLY.filter((c) => tui.includes(c) || !names.includes(c))).toEqual([]);
  });
  it("/theme names every theme the TUI and web share, and slash.ts accepts each", () => {
    const hint = ACTIONS.find((a) => a.name === "theme")!.hint;
    for (const t of THEMES) expect(hint).toContain(t.name);
    expect(popup).toContain("[" + THEMES.map((t) => t.name).join("|") + "]");
    expect(slash).not.toMatch(/arg === "plum"/);
  });
  it("lists every command slash.ts handles, so none goes to the agent as text", () => {
    const handled = [...slash.matchAll(/\ba === "(\w+)"/g)].map((m) => m[1]);
    expect(handled).toContain("bench");
    expect(handled.filter((h) => !names.includes(h))).toEqual([]);
  });
  it("/mode picks the transcript detail the way the TUI does", () => {
    expect(nextDetail("expandable", "verbose")).toBe("verbose");
    expect(nextDetail("minimal", "")).toBe("expandable");
    expect(nextDetail("verbose", "")).toBe("minimal");
    expect(nextDetail("minimal", "dark")).toBe("expandable");
  });
});
