import { beforeEach, describe, expect, it, vi } from "vitest";

const calls: string[] = [];
vi.mock("../../lib/actions", () => ({
  setSetting: async (k: string, v: string) => void calls.push("set " + k + " " + v),
  unsetSetting: async (k: string) => void calls.push("unset " + k),
  setSessionModel: async (id: string, m: string, e: string) => void calls.push("session " + id + " " + m + " " + e),
}));
vi.mock("../../lib/router", () => ({ go: (r: { name: string }) => void calls.push("go " + r.name) }));

const { run } = await import("./slash");

function ctx(s?: { id: string; agent: string; model?: string }) {
  const out = { note: "", shimmer: 0, pick: false };
  const c = {
    s: s as never, pend: [], log: [], detail: "expandable" as const,
    setPick: (o: boolean) => (out.pick = o), setNote: (n: string) => (out.note = n), setAccent: () => {},
    setDetail: () => {}, setInfo: () => {}, showTend: () => {}, shimmer: () => out.shimmer++,
  };
  return { c, out };
}
const tick = () => new Promise((r) => setTimeout(r, 0));

describe("/effort", () => {
  beforeEach(() => void (calls.length = 0));
  it("sets agent.effort, the next session's default, when no session is open", async () => {
    const { c, out } = ctx();
    run("effort", "low", c);
    await tick();
    expect(calls).toEqual(["set agent.effort low"]);
    expect(out.note).toBe("saved agent.effort = low: the next session starts with it");
    expect(out.shimmer).toBe(0);
  });
  it("puts agent.effort back with off or default, and shimmers on the top level", async () => {
    const { c, out } = ctx();
    run("effort", "off", c);
    run("effort", "default", c);
    run("effort", "medium", c);
    await tick();
    expect(calls).toEqual(["unset agent.effort", "unset agent.effort", "set agent.effort medium"]);
    expect(out.shimmer).toBe(1);
  });
  it("refuses a level smithy does not allow", async () => {
    const { c, out } = ctx();
    run("effort", "high", c);
    await tick();
    expect(calls).toEqual([]);
    expect(out.note).toBe("effort must be off or one of low, medium (smithy caps effort at medium)");
  });
  it("sets the open session's effort from a typed level, and opens the picker without one", async () => {
    const { c, out } = ctx({ id: "s-1", agent: "claude", model: "opus" });
    run("effort", "medium", c);
    await tick();
    expect(calls).toEqual(["session s-1 opus medium"]);
    expect(out.shimmer).toBe(1);
    run("effort", "", c);
    expect(out.pick).toBe(true);
  });
  it("tells an agent without an effort flag that it keeps its own", () => {
    const { c, out } = ctx({ id: "s-2", agent: "opencode" });
    run("effort", "low", c);
    expect(out.note).toBe("opencode keeps its own effort; /effort works for claude and codex");
  });
});
