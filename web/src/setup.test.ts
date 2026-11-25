import { describe, expect, it } from "vitest";
import { choice, isDefault, toggleSkill, type Ready } from "./setup";

const claude: Ready = { name: "claude", kind: "cli", ready: true, detail: "/bin/claude" };
const anth: Ready = { name: "api:anthropic", kind: "api", ready: true, detail: "smithy auth" };

describe("setup", () => {
  it("a CLI keeps its model apart; an API agent carries it in its name", () => {
    expect(choice(claude, " opus ")).toEqual({ agent: "claude", model: "opus" });
    expect(choice(claude, "")).toEqual({ agent: "claude", model: "" });
    expect(choice(anth, "claude-sonnet-5-5")).toEqual({ agent: "api:anthropic/claude-sonnet-5-5", model: "" });
    expect(choice(anth, "  ")).toBeNull();
  });
  it("marks the default row, api rows by provider", () => {
    expect(isDefault(claude, { agent: "claude" })).toBe(true);
    expect(isDefault(anth, { agent: "api:anthropic/x" })).toBe(true);
    expect(isDefault(anth, { agent: "api:anthropicx/y" })).toBe(false);
    expect(isDefault(claude, { agent: "codex" })).toBe(false);
  });
  it("toggles a skill in or out of the kit without touching mcp", () => {
    expect(toggleSkill({ mcp: false, skills: [] }, "hush")).toEqual({ mcp: false, skills: ["hush"] });
    expect(toggleSkill({ mcp: true, skills: ["hush", "smelt"] }, "hush")).toEqual({ mcp: true, skills: ["smelt"] });
  });
});
