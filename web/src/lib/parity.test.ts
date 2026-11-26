import { describe, expect, it } from "vitest";
import { clock, controlOf, effortOptions, setEffortRules, groupSettings, headerLine, intent, modelEditable, withImages } from "./parity";

describe("effort, the same rules as the TUI", () => {
  it("offers default, low and medium only to agents with an effort flag", () => {
    expect(effortOptions("claude")).toEqual(["", "low", "medium"]);
    expect(effortOptions("codex")).toEqual(["", "low", "medium"]);
    expect(effortOptions("acp:claude")).toEqual(["", "low", "medium"]);
    expect(effortOptions("opencode")).toEqual([]);
    expect(effortOptions("api:anthropic/claude-x")).toEqual([]);
  });
  it("reads the effort rules from the daemon, with the fallback for older daemons", () => {
    setEffortRules({ agents: ["gemini"], levels: ["low", "medium", "high"], max: "high" });
    expect(effortOptions("gemini")).toEqual(["", "low", "medium", "high"]);
    expect(effortOptions("claude")).toEqual([]);
    setEffortRules(undefined);
    expect(effortOptions("claude")).toEqual(["", "low", "medium"]);
    expect(effortOptions("gemini")).toEqual([]);
  });
  it("keeps an API session's model fixed: it is part of its agent", () => {
    expect(modelEditable("claude")).toBe(true);
    expect(modelEditable("api:openai/gpt")).toBe(false);
  });
});

describe("intent", () => {
  it("runs !cmd in the shell, never sends it", () => {
    expect(intent("!git status")).toEqual({ kind: "shell", cmd: "git status" });
    expect(intent("!  ")).toEqual({ kind: "send", text: "!" });
  });
  it("runs a smithy command with its argument here", () => {
    expect(intent("/effort low")).toEqual({ kind: "action", name: "effort", arg: "low" });
    expect(intent(" /stop ")).toEqual({ kind: "action", name: "stop", arg: "" });
    expect(intent("/set warden.max 3")).toEqual({ kind: "action", name: "set", arg: "warden.max 3" });
  });
  it("sends skills and prose to the agent", () => {
    expect(intent("/smelt fix ci")).toEqual({ kind: "send", text: "/smelt fix ci" });
    expect(intent("hello")).toEqual({ kind: "send", text: "hello" });
  });
});

describe("withImages", () => {
  it("lists attached images the way the TUI does", () => {
    expect(withImages("look", [])).toBe("look");
    expect(withImages("look", ["/t/a.png", "/t/b.png"])).toBe("look\n\nAttached images:\n/t/a.png\n/t/b.png");
  });
});

describe("settings", () => {
  const row = (key: string, kind: string, extra = {}) => ({ key, value: "", src: "default", default: "", meaning: "", kind, ...extra });
  it("picks a typed control per kind", () => {
    expect(controlOf(row("guard.enabled", "bool"))).toBe("toggle");
    expect(controlOf(row("warden.merge", "enum", { options: ["auto", "manual"] }))).toBe("select");
    expect(controlOf(row("warden.max", "number"))).toBe("number");
    expect(controlOf(row("guard.block", "list"))).toBe("list");
    expect(controlOf(row("linear.team", "text"))).toBe("text");
    expect(controlOf(row("notify.telegram.token", "text", { secret: true }))).toBe("secret");
  });
  it("groups keys by their first word, in first-seen order", () => {
    const g = groupSettings([row("warden.max", "number"), row("agent.effort", "enum"), row("warden.mode", "text")]);
    expect(g.map((x) => [x.group, x.rows.map((r) => r.key)])).toEqual([
      ["warden", ["warden.max", "warden.mode"]],
      ["agent", ["agent.effort"]],
    ]);
  });
});

describe("header", () => {
  it("reads like the TUI's: ◆ smithy version · project", () => {
    expect(headerLine("0.25.0", "/home/me/code/skills")).toBe("◆ smithy v0.25.0 · skills");
    expect(headerLine("", "")).toBe("◆ smithy");
    expect(headerLine("v0.25.0", "")).toBe("◆ smithy v0.25.0");
    expect(headerLine("dev", "")).toBe("◆ smithy dev");
  });
  it("shows the clock as HH:MM", () => {
    expect(clock(new Date(2026, 0, 2, 9, 5))).toBe("09:05");
  });
});
