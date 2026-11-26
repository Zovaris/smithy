import { describe, expect, it } from "vitest";
import popup from "../../../internal/tui/chat_popup.go?raw";
import slash from "../features/chat/slash.ts?raw";
import { ACTIONS, action, nextDetail, suggest, TUI_ONLY } from "./commands";

const names = ACTIONS.map((a) => a.name);

describe("commands", () => {
  it("suggests actions and skills for a bare /word, prefix matches first", () => {
    expect(suggest("/st", ["smelt", "tend"]).map((c) => c.name)).toEqual(["stop"]);
    expect(suggest("/se", ["smelt", "scout"]).map((c) => c.name)).toEqual(["sessions", "setup", "serve", "set", "settings", "unset"]);
    expect(suggest("/", []).length).toBe(ACTIONS.length);
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
