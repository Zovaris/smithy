import { describe, expect, it } from "vitest";
import { groupAgents, projectOf, startDir } from "./projects";
import type { Ready } from "./setup";
import type { Sess } from "./store";

const r = (name: string, kind: "cli" | "api", ready: boolean, fix = ""): Ready => ({ name, kind, ready, detail: "/bin/" + name, fix });
const s = (id: string, status: Sess["status"], updated: string, dir = "/w/a"): Sess => ({ id, agent: "claude", dir, title: id, status, created: updated, updated });

describe("groupAgents", () => {
  it("splits ready CLIs, ready API agents and missing ones with their fix", () => {
    const g = groupAgents([r("claude", "cli", true), r("codex", "cli", false, "npm i -g codex"), r("api:openai", "api", true), r("api:deepseek", "api", false, "smithy auth set deepseek")], []);
    expect(g.clis.map((x) => x.name)).toEqual(["claude"]);
    expect(g.apis.map((x) => x.name)).toEqual(["api:openai"]);
    expect(g.missing.map((x) => [x.name, x.fix])).toEqual([["codex", "npm i -g codex"], ["api:deepseek", "smithy auth set deepseek"]]);
  });
  it("puts working sessions first, then the most recent", () => {
    const g = groupAgents([], [s("old", "done", "2026-01-01"), s("run", "working", "2025-01-01"), s("new", "done", "2026-02-01"), s("ask", "needs_you", "2024-01-01")]);
    expect(g.sessions.map((x) => x.id)).toEqual(["run", "ask", "new", "old"]);
  });
});

describe("projects", () => {
  const l = { projects: [{ dir: "/w/a", name: "a" }, { dir: "/w/b", name: "b" }], default: "/w/b" };
  it("names a session's project by its folder, or the folder's base name", () => {
    expect(projectOf(l, "/w/a")).toBe("a");
    expect(projectOf(l, "/x/other")).toBe("other");
  });
  it("starts in the remembered project, else the default, else the first", () => {
    expect(startDir(l, "/w/a")).toBe("/w/a");
    expect(startDir(l, "/gone")).toBe("/w/b");
    expect(startDir({ projects: l.projects }, "")).toBe("/w/a");
    expect(startDir({ projects: [] }, "")).toBe("");
  });
});
