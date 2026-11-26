import { describe, expect, it } from "vitest";
import { count, rate, stats, usd } from "./stats";
import { blank, fold, type Store } from "./store";
import { blocked, moved, next, rel, type TaskBoard } from "./tasks";

let seq = 0;
const ev = (kind: string, data: unknown, session: string, time: string) => ({ seq: ++seq, time, kind, session, data });
const now = Date.parse("2026-11-26T12:00:00Z");

function store(): Store {
  return [
    ev("session_started", { id: "a", agent: "claude", dir: "/r", title: "one", status: "working" }, "a", "2026-11-26T11:30:00Z"),
    ev("session_usage", { input_tokens: 1000, output_tokens: 600, cost_usd: 0.5 }, "a", "2026-11-26T11:31:00Z"),
    ev("session_status", { status: "done" }, "a", "2026-11-26T11:31:00Z"),
    ev("session_started", { id: "b", agent: "codex", dir: "/r", title: "two", status: "working" }, "b", "2026-11-26T09:10:00Z"),
    ev("session_started", { id: "c", agent: "claude", dir: "/r", title: "old", status: "done" }, "c", "2026-11-24T09:10:00Z"),
    ev("decision_asked", { id: "perm:b/1", question: "ok?" }, "b", "2026-11-26T09:11:00Z"),
  ].reduce(fold, blank());
}

describe("stats", () => {
  it("sums cost and tokens, counts runs per hour in the last 24 h", () => {
    const s = stats(store(), now);
    expect(s.runs).toBe(3);
    expect(s.working).toBe(1);
    expect(s.waiting).toBe(1);
    expect(s.cost).toBe(0.5);
    expect([s.input, s.output]).toEqual([1000, 600]);
    expect(s.hourly.reduce((a, b) => a + b)).toBe(2);
    expect(s.hourly[23]).toBe(1);
    expect(s.hourly[21]).toBe(1);
    expect(s.costBy.map((x) => x.id)).toEqual(["a"]);
  });
  it("reads tok/s from output tokens over wall time", () => {
    const st = store();
    expect(rate(st.sessions.a)).toBe(10);
    expect(rate(st.sessions.b)).toBeNull();
    expect(stats(st, now).tps).toBe(10);
  });
  it("has no cost when no session reported one", () => {
    expect(stats(blank(), now).cost).toBeNull();
    expect(usd(null)).toBe("?");
    expect(count(1530)).toBe("1.5k");
  });
});

describe("tasks", () => {
  const b: TaskBoard = {
    project: "/p", statuses: ["todo", "doing", "done", "dropped"],
    lists: [{ dir: "/p/docs/specs/v1", name: "v1", tasks: [
      { file: "/p/docs/specs/v1/01-a.md", nn: "01", title: "A", status: "doing", blockers: [] },
      { file: "/p/docs/specs/v1/02-b.md", nn: "02", title: "B", status: "todo", blockers: ["01"] },
    ] }],
  };
  it("moves a card and unblocks what waited on it", () => {
    const l = b.lists[0];
    expect(blocked(l, l.tasks[1])).toBe(true);
    const m = moved(b, l.tasks[0].file, "done");
    expect(m.lists[0].tasks[0].status).toBe("done");
    expect(blocked(m.lists[0], m.lists[0].tasks[1])).toBe(false);
    expect(b.lists[0].tasks[0].status).toBe("doing");
  });
  it("names files from the project and steps columns", () => {
    expect(rel("/p/docs/specs/v1/01-a.md", "/p")).toBe("docs/specs/v1/01-a.md");
    expect([next("todo", 1), next("todo", -1), next("dropped", 1)]).toEqual(["doing", "", ""]);
  });
});
