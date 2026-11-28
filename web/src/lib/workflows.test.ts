import { describe, expect, it } from "vitest";
import { result, state, type Workflow } from "./workflows";

const w = (o: Partial<Workflow>): Workflow => ({ name: "d", schedule: "0 17 * * *", steps: [], file: "/x/d.json", enabled: false, ...o });

describe("workflows", () => {
  it("names the state the way the TUI does", () => {
    expect(state(w({}), [])).toBe("off");
    expect(state(w({ enabled: true }), [])).toBe("on");
    expect(state(w({ changed: true }), [])).toBe("changed");
    expect(state(w({ error: "bad" }), [])).toBe("error");
    expect(state(w({ enabled: true }), ["d"])).toBe("running");
  });
  it("writes a step result as one line", () => {
    expect(result({ id: "a", kind: "run", exit: 2, output: "FAIL\n  x" })).toBe("exit 2 · FAIL x");
    expect(result({ id: "i", kind: "if", exit: 0, branch: "stop" })).toBe("→ stop");
    expect(result({ id: "j", kind: "agent", exit: 0, answer: "yes", why: "patch bump" })).toBe("yes patch bump");
    expect(result({ id: "j", kind: "agent", exit: 0, error: "timed out" })).toBe("error: timed out");
  });
});
