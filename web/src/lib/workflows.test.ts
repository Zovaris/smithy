import { describe, expect, it } from "vitest";
import { ms, result, state, summary, triggers, type Workflow } from "./workflows";

const w = (o: Partial<Workflow>): Workflow => ({ name: "d", schedule: "0 17 * * *", steps: [], file: "/x/d.json", enabled: false, ...o });

describe("workflows", () => {
  it("names the state the way the TUI does", () => {
    expect(state(w({}), [])).toBe("off");
    expect(state(w({ enabled: true }), [])).toBe("on");
    expect(state(w({ changed: true }), [])).toBe("changed");
    expect(state(w({ error: "bad" }), [])).toBe("error");
    expect(state(w({ enabled: true }), ["d"])).toBe("running");
  });
  it("lists the triggers", () => {
    expect(triggers(w({ webhook: true }))).toBe("manual · 0 17 * * * · webhook");
    expect(triggers(w({ schedule: "" }))).toBe("manual");
  });
  it("writes a step and its result as one line", () => {
    expect(summary({ id: "i", kind: "if", if: { step: "a", match: "\\S" }, else: "stop" })).toBe("a ~ /\\S/ → next, else stop");
    expect(summary({ id: "n", kind: "notify", message: "m", when: { step: "a", exit: 0 } })).toBe(": m (when a exit 0)");
    expect(result({ id: "a", kind: "run", status: "ok", duration_ms: 3, exit: 2, output: "FAIL\n  x" })).toBe("exit 2 · FAIL x");
    expect(result({ id: "i", kind: "if", status: "ok", duration_ms: 0, exit: 0, branch: "stop" })).toBe("→ stop");
    expect(result({ id: "j", kind: "agent", status: "ok", duration_ms: 0, exit: 0, answer: "yes", why: "patch bump" })).toBe("yes patch bump");
    expect(result({ id: "s", kind: "notify", status: "skipped", duration_ms: 0, exit: 0 })).toBe("skipped");
    expect([ms(40), ms(2500), ms(180000)]).toEqual(["40ms", "2.5s", "3m"]);
  });
});
