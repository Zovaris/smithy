import { describe, expect, it } from "vitest";
import { choice, isDefault, modelRows, toggleSkill, type ModelList, type Ready } from "./setup";

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

describe("model rows", () => {
  const list: ModelList = {
    agent: "opencode", source: "ACP session/new", fetched: "",
    models: [{ id: "oc/a", name: "Alpha", default: true }, { id: "oc/b", name: "Beta", in_per_m: 3, out_per_m: 15 }],
  };
  it("a CLI starts with the agent default, then what it reports", () => {
    expect(modelRows(list, "", "cli").map((r) => r.id)).toEqual(["", "oc/a", "oc/b"]);
  });
  it("an API agent has no default row", () => {
    expect(modelRows(list, "", "api").map((r) => r.id)).toEqual(["oc/a", "oc/b"]);
  });
  it("filters by id or name and offers Other only when nothing matches exactly", () => {
    expect(modelRows(list, "beta", "cli").map((r) => [r.id, !!r.other])).toEqual([["oc/b", false], ["beta", true]]);
    expect(modelRows(list, "oc/b", "cli").map((r) => r.id)).toEqual(["oc/b"]);
    expect(modelRows(null, "mine", "cli")).toEqual([{ id: "mine", label: "Other: mine", hint: "use this name as typed", other: true }]);
  });
  it("shows the price when the source gives one", () => {
    expect(modelRows(list, "oc/b", "cli")[0].hint).toBe("Beta · $3.00/$15.00 per M");
  });
});
