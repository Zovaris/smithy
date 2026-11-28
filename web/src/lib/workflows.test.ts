import { describe, expect, it } from "vitest";
import { ms, opensSession, result, state, summary, trigger, type Schedule } from "./workflows";

const sc = (o: Partial<Schedule>): Schedule => ({ name: "d", workflow: "d", cron: "0 17 * * *", file: "/x/d.json", enabled: false, ...o });

describe("workflows", () => {
  it("names a schedule's state and trigger the way the TUI does", () => {
    expect(state(sc({}))).toBe("off");
    expect(state(sc({ enabled: true }))).toBe("on");
    expect(state(sc({ changed: true }))).toBe("changed");
    expect(state(sc({ error: "bad" }))).toBe("error");
    expect(trigger(sc({}))).toBe("0 17 * * *");
    expect(trigger(sc({ cron: "", github: { event: "issue_labeled", label: "bug" } }))).toBe("github issue_labeled bug");
    expect(trigger(sc({ cron: "", watch: "~/x" }))).toBe("watch ~/x");
  });
  it("writes a step and its result as one line", () => {
    expect(summary({ id: "i", kind: "if", if: { step: "a", match: "\\S" }, else: "stop" })).toBe("a ~ /\\S/ → next, else stop");
    expect(summary({ id: "n", kind: "notify", message: "m", when: { step: "a", exit: 0 } })).toBe(": m (when a exit 0)");
    expect(result({ id: "a", kind: "run", status: "ok", duration_ms: 3, exit: 2, output: "FAIL\n  x" })).toBe("exit 2 · FAIL x");
    expect(result({ id: "i", kind: "if", status: "ok", duration_ms: 0, exit: 0, branch: "stop" })).toBe("→ stop");
    expect(result({ id: "j", kind: "agent", status: "ok", duration_ms: 0, exit: 0, answer: "yes", why: "patch bump" })).toBe("yes patch bump");
    expect(result({ id: "s", kind: "notify", status: "skipped", duration_ms: 0, exit: 0 })).toBe("skipped");
    expect([ms(40), ms(2500), ms(180000)]).toEqual(["40ms", "2.5s", "3m"]);
    expect(summary({ id: "l", kind: "loop", do: [{ id: "t", kind: "run" }], until: { step: "t", exit: 0 } })).toBe("1 steps until t exit 0");
    expect(summary({ id: "a", kind: "agent", items: "{{steps.x.output}}", prompt: "review {{item}}" })).toBe("each of {{steps.x.output}}: review {{item}}");
    expect(opensSession({ id: "s", kind: "smelt", status: "ok", duration_ms: 0, exit: 0, output: "s-1" })).toBe(true);
  });
});
