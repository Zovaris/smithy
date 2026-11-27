import { describe, expect, it } from "vitest";
import type { Ev } from "./model";
import { agentName, ago, argLine, blank, fold, initials, kindOf, open, primary, seed, sessionOf, sessions, setNames, type Store } from "./store";
import { parse, totals, withComments } from "./diff";

let seq = 0;
const ev = (kind: string, data: unknown, session = "s-1", time = "2026-11-25T10:00:00Z"): Ev => ({ seq: ++seq, time, kind, session, data });
const run = (...es: Ev[]): Store => es.reduce(fold, blank());
const started = (id = "s-1", title = "Add retry") => ev("session_started", { id, agent: "claude", model: "sonnet", dir: "/r", title, status: "done" }, id);

describe("fold", () => {
  it("keeps the daemon's log lines for /log, newest last and at most 200", () => {
    const lines = Array.from({ length: 205 }, (_, i) => ev("log", { line: "daemon: " + i }, ""));
    const s = run(...lines);
    expect(s.log.length).toBe(200);
    expect(s.log[199]).toBe("daemon: 204");
  });
  it("pins a session's goal and notes each change in the conversation", () => {
    const s = run(started(), ev("session_goal", { goal: "ship it" }), ev("session_goal", { goal: "ship it", state: "met", turns: 2 }));
    expect([s.sessions["s-1"].goal, s.sessions["s-1"].goal_state]).toEqual(["ship it", "met"]);
    const said = (st: Store) => st.convo["s-1"].flatMap((i) => (i.kind === "agent" ? i.parts : [])).map((p) => (p.kind === "text" ? p.text.trim() : ""));
    expect(said(s)).toEqual(["goal set: ship it", "goal met"]);
    const c = run(started(), ev("session_goal", { goal: "x" }), ev("session_goal", { goal: "" }));
    expect(c.sessions["s-1"].goal).toBe("");
    expect(said(c)).toEqual(["goal set: x", "goal cleared"]);
  });
  it("keeps a session's model and effort when /model or /effort changes them", () => {
    const s = run(started(), ev("session_model", { model: "opus", effort: "low" }));
    expect([s.sessions["s-1"].model, s.sessions["s-1"].effort]).toEqual(["opus", "low"]);
  });
  it("starts a session and moves the latest updated to the top", () => {
    const s = run(started("s-1", "one"), started("s-2", "two"), ev("session_status", { status: "working" }, "s-1"));
    expect(sessions(s).map((x) => [x.id, x.status])).toEqual([["s-1", "working"], ["s-2", "done"]]);
  });
  it("ignores events at or below the last seq", () => {
    const a = started();
    const s = fold(run(a), a);
    expect(s.order).toEqual(["s-1"]);
  });
  it("streams text, groups tools and skips the duplicate final reply", () => {
    const s = run(
      started(),
      ev("session_message", { role: "user", text: "add retry" }),
      ev("session_text", { text: "Reading " }),
      ev("session_text", { text: "the sender." }),
      ev("session_tool", { name: "Read", args: { file_path: "a.go" } }),
      ev("session_tool", { name: "Edit", args: { file_path: "a.go" } }),
      ev("session_text", { text: "Done." }),
      ev("session_message", { role: "agent", text: "Reading the sender.Done." }),
    );
    const c = s.convo["s-1"];
    expect(c.map((i) => i.kind)).toEqual(["you", "agent"]);
    const parts = (c[1] as Extract<typeof c[1], { kind: "agent" }>).parts;
    expect(parts.map((p) => p.kind)).toEqual(["text", "tools", "text"]);
    expect(parts[0]).toEqual({ kind: "text", text: "Reading the sender." });
  });
  it("lets a different final reply replace noisy streamed text", () => {
    const s = run(started(), ev("session_text", { text: 'ms":1}\n' }), ev("session_tool", { name: "Read" }), ev("session_message", { role: "agent", text: "Clean." }));
    expect((s.convo["s-1"][0] as any).parts).toEqual([{ kind: "tools", tools: [{ name: "Read" }] }, { kind: "text", text: "Clean." }]);
  });
  it("uses the final reply when nothing streamed", () => {
    const s = run(started(), ev("session_message", { role: "user", text: "hi" }), ev("session_message", { role: "agent", text: "hello" }));
    expect(s.convo["s-1"][1]).toMatchObject({ kind: "agent", parts: [{ kind: "text", text: "hello" }] });
  });
  it("puts a decision inline in its session and closes it when answered", () => {
    let s = run(started(), ev("decision_asked", { id: "perm:s-1/1", question: "Run rm?", options: ["run once", "always allow this pattern", "deny"] }));
    expect(open(s).map((d) => [d.id, d.session])).toEqual([["perm:s-1/1", "s-1"]]);
    expect(s.convo["s-1"][0]).toMatchObject({ kind: "agent", parts: [{ kind: "decision", id: "perm:s-1/1" }] });
    s = fold(s, ev("decision_answered", { id: "perm:s-1/1", answer: "deny", by: "me" }, ""));
    expect(open(s)).toEqual([]);
    expect(s.decisions[0]).toMatchObject({ answer: "deny", by: "me" });
  });
  it("sums usage and keeps the latest diff", () => {
    const s = run(started(), ev("session_usage", { input_tokens: 10, output_tokens: 2, cost_usd: 0.1 }), ev("session_usage", { input_tokens: 5, output_tokens: 1 }), ev("session_diff", { stat: "1 file", patch: "p1" }), ev("session_diff", { stat: "2 files", patch: "p2" }));
    expect(s.sessions["s-1"].usage).toEqual({ input_tokens: 15, output_tokens: 3, cost_usd: 0.1 });
    expect(s.diffs["s-1"].patch).toBe("p2");
  });
  it("leaves cost unknown when no event priced it", () => {
    const s = run(started(), ev("session_usage", { input_tokens: 1, output_tokens: 1 }));
    expect(s.sessions["s-1"].usage?.cost_usd).toBeUndefined();
  });
  it("seeds pending decisions once", () => {
    const s = seed(seed(blank(), [{ id: "guard:ab", question: "Run x?" }]), [{ id: "guard:ab", question: "Run x?" }]);
    expect(s.decisions).toHaveLength(1);
    expect(s.decisions[0].options).toEqual(["yes", "no"]);
  });
});

describe("helpers", () => {
  it("finds the session of a permission decision", () => {
    expect(sessionOf("perm:s-9/2")).toBe("s-9");
    expect(sessionOf("guard:ab")).toBe("");
    expect(sessionOf("guard:ab", "s-3")).toBe("s-3");
  });
  it("names agents by what the daemon serves, else the raw name", () => {
    setNames([]);
    expect(agentName("pi")).toBe("pi");
    expect(agentName("acp:dsh")).toBe("dsh");
    setNames([{ name: "pi", display: "Pi" }, { name: "dsh", display: "dsh" }, { name: "api:anthropic", display: "Anthropic API" }, { name: "zed" }]);
    expect(agentName("pi")).toBe("Pi");
    expect(agentName("api:anthropic/claude-sonnet-5-5")).toBe("Anthropic API");
    expect(agentName("zed")).toBe("zed");
    expect(agentName("")).toBe("agent");
  });
  it("names agents and decision kinds", () => {
    setNames([{ name: "claude", display: "Claude Code" }, { name: "codex", display: "Codex" }, { name: "opencode", display: "OpenCode" }, { name: "api:anthropic", display: "Anthropic API" }]);
    expect(agentName("claude")).toBe("Claude Code");
    expect(agentName("acp:codex")).toBe("Codex");
    expect(agentName("api:anthropic/claude-sonnet-5-5")).toBe("Anthropic API");
    expect(initials("claude")).toBe("CC");
    expect(initials("opencode")).toBe("OP");
    const d = { id: "guard:1", session: "", question: "Run git reset --hard?", options: ["run once", "deny"], asked: "" };
    expect(kindOf(d)).toBe("guard · blocked command");
    expect(primary(d)).toBe("deny");
    expect(primary({ ...d, options: ["merge", "later"] })).toBe("merge");
  });
  it("reads a tool's main argument", () => {
    expect(argLine({ name: "Bash", args: { command: "go test ./..." } })).toBe("go test ./...");
    expect(argLine({ name: "Read", args: '{"file_path":"a.go"}' })).toBe("a.go");
    expect(argLine({ name: "X" })).toBe("");
  });
  it("says how long ago", () => {
    const now = Date.parse("2026-11-25T12:00:00Z");
    expect(ago("2026-11-25T11:59:50Z", now)).toBe("now");
    expect(ago("2026-11-25T11:48:00Z", now)).toBe("12 min ago");
    expect(ago("2026-11-25T10:00:00Z", now)).toBe("2 h ago");
  });
});

describe("diff", () => {
  const patch = [
    "diff --git a/notify/webhook.go b/notify/webhook.go",
    "index 1..2 100644",
    "--- a/notify/webhook.go",
    "+++ b/notify/webhook.go",
    "@@ -40,3 +40,4 @@ func Send",
    " ctx := x",
    "-  return post()",
    "+  for i := 0; i < 3; i++ {",
    "+  }",
    "diff --git a/old.txt b/old.txt",
    "deleted file mode 100644",
    "--- a/old.txt",
    "+++ /dev/null",
    "@@ -1 +0,0 @@",
    "-gone",
  ].join("\n");
  it("splits files and counts lines with numbers", () => {
    const fs = parse(patch);
    expect(fs.map((f) => [f.path, f.add, f.del])).toEqual([["notify/webhook.go", 2, 1], ["old.txt", 0, 1]]);
    expect(fs[0].lines.map((l) => [l.t, l.n])).toEqual([["hunk", undefined], ["ctx", 40], ["del", 41], ["add", 41], ["add", 42]]);
    expect(totals(fs)).toEqual({ add: 2, del: 2 });
  });
  it("puts diff comments before the next message", () => {
    expect(withComments("go on", [{ path: "a.go", line: 4, text: "use ctx" }])).toBe("Comments on the diff:\n- a.go:4: use ctx\n\ngo on");
    expect(withComments("go on", [])).toBe("go on");
  });
});

import { split } from "../components/ui";
describe("guard questions", () => {
  it("pull the command or host out as code", () => {
    expect(split("Run this command? git reset --hard origin/main")).toEqual({ title: "Run a command that matches the guard?", code: "git reset --hard origin/main" });
    expect(split("Fetch from evil.example?")).toEqual({ title: "Fetch from a host the guard blocks?", code: "evil.example" });
    expect(split("Merge #12?")).toEqual({ title: "Merge #12?", code: "" });
  });
});

import { prose } from "./store";
describe("prose", () => {
  it("drops stream-json lines a CLI log leaks and keeps the words", () => {
    expect(prose('{"type":"system"}\nDone: retry added.\n{"result":"x","n":1}\n{not json}')).toBe("Done: retry added.\n{not json}");
  });
});

import { blocks } from "../components/md";
describe("md blocks", () => {
  it("splits paragraphs, lists and fences", () => {
    expect(blocks("Done.\n\n- one\n- two\n1. a\n```\nx := 1\n```\nend").map((b) => [b.t, b.lines.length])).toEqual([["p", 1], ["ul", 2], ["ol", 1], ["pre", 1], ["p", 1]]);
  });
});
