import { describe, expect, it } from "vitest";
import { apply, board, empty, parseSSE, rows, say, sessionState, type Ev } from "./model";

const ev = (seq: number, kind: string, data: unknown = {}, session = ""): Ev => ({ seq, time: "2026-11-24T10:00:00Z", kind, session, data });
const tool = (seq: number, name: string, status = "ok", session = "s1") => ev(seq, "tool", { name, status, lines: ["out"] }, session);

describe("parseSSE", () => {
  it("splits frames, keeps the tail and ignores pings", () => {
    const r = parseSSE('id: 1\nevent: log\ndata: {"seq":1}\n\n: ping\n\nid: 2\nda');
    expect(r.frames).toEqual([{ id: "1", data: '{"seq":1}' }]);
    expect(r.rest).toBe("id: 2\nda");
  });
  it("accepts CRLF", () => {
    expect(parseSSE("id: 3\r\ndata: x\r\n\r\n").frames).toEqual([{ id: "3", data: "x" }]);
  });
});

describe("apply", () => {
  it("ignores events at or below the last seq", () => {
    let s = apply(empty(), ev(2, "log", { line: "a" }));
    s = apply(s, ev(2, "log", { line: "b" }));
    s = apply(s, ev(1, "log", { line: "c" }));
    expect(s.msgs.map((m) => m.text)).toEqual(["a"]);
    expect(s.last).toBe(2);
  });
  it("tracks sessions with the TUI's states", () => {
    let s = apply(empty(), ev(1, "launched", {}, "a"));
    s = apply(s, ev(2, "failed", {}, "b"));
    s = apply(s, ev(3, "merged", {}, "c"));
    s = apply(s, ev(4, "decision_asked", { id: "d1", question: "Merge?" }, "d"));
    expect(s.sessions.map((x) => [x.id, x.state])).toEqual([["a", "running"], ["b", "failed"], ["c", "done"], ["d", "waiting"]]);
  });
  it("pins asked decisions and drops answered ones", () => {
    let s = apply(empty(), ev(1, "decision_asked", { id: "d1", question: "Merge #142?", options: ["merge", "later"] }));
    expect(s.pending.map((d) => d.id)).toEqual(["d1"]);
    expect(s.msgs.at(-1)?.text).toBe("? Merge #142?");
    s = apply(s, ev(2, "decision_answered", { id: "d1", answer: "merge", by: "me@x" }));
    expect(s.pending).toEqual([]);
    expect(s.msgs.at(-1)?.text).toBe("answered: merge by me@x");
  });
  it("keeps tool, thinking and turn as their own kinds", () => {
    let s = apply(empty(), tool(1, "Read"));
    s = apply(s, ev(2, "thinking", { text: "hmm" }));
    s = apply(s, ev(3, "turn", { text: "12s" }));
    expect(s.msgs.map((m) => m.kind)).toEqual(["tool", "thinking", "turn"]);
    expect(s.msgs[0].tool?.name).toBe("Read");
  });
  it("names other kinds with their session", () => {
    expect(apply(empty(), ev(1, "queued", {}, "TK-5")).msgs[0].text).toBe("queued TK-5");
  });
  it("say adds a local you message", () => {
    expect(say(empty(), "hi").msgs[0]).toMatchObject({ who: "you", text: "hi" });
  });
});

describe("rows", () => {
  const s = [tool(1, "Read"), tool(2, "Grep", "failed"), ev(3, "thinking", { text: "t" }), tool(4, "Edit", "running")].reduce(apply, empty());
  it("minimal folds a tool run into one summary with the worst status", () => {
    const r = rows(s.msgs, "minimal", new Set(), "");
    expect(r.map((x) => x.type)).toEqual(["summary", "summary"]);
    expect(r[0]).toMatchObject({ names: ["Read", "Grep"], status: "failed" });
    expect(r[1]).toMatchObject({ names: ["Edit"], status: "running" });
  });
  it("expandable shows one header per tool, open only when toggled", () => {
    const r = rows(s.msgs, "expandable", new Set([1]), "");
    expect(r.map((x) => x.type)).toEqual(["tool", "tool", "tool"]);
    expect(r.map((x) => x.type === "tool" && x.open)).toEqual([false, true, false]);
  });
  it("verbose opens every tool and shows thinking", () => {
    const r = rows(s.msgs, "verbose", new Set(), "");
    expect(r.map((x) => x.type)).toEqual(["tool", "tool", "text", "tool"]);
    expect(r.every((x) => x.type !== "tool" || x.open)).toBe(true);
  });
  it("filters by session", () => {
    const t = apply(s, ev(5, "log", { line: "other" }, "s2"));
    expect(rows(t.msgs, "verbose", new Set(), "s2").length).toBe(1);
  });
});

describe("board", () => {
  it("groups sessions into columns", () => {
    let s = apply(empty(), ev(1, "launched", {}, "a"));
    s = apply(s, ev(2, "decision_asked", { id: "d" }, "b"));
    s = apply(s, ev(3, "merged", {}, "c"));
    const b = board(s.sessions);
    expect(b.map((c) => [c.state, c.items.map((x) => x.id)])).toEqual([["running", ["a"]], ["waiting", ["b"]], ["done", ["c"]], ["failed", []]]);
  });
  it("sessionState matches the TUI", () => {
    expect(["error", "stopped", "x"].map(sessionState)).toEqual(["failed", "done", "running"]);
  });
});
