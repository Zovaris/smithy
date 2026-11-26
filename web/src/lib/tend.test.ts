import { describe, expect, it } from "vitest";
import { tendRows } from "./tend";

describe("tendRows", () => {
  it("names each PR's session or batch item, its status and tend's next step, like the TUI's /tend", () => {
    const rows = tendRows({
      mode: "fix",
      prs: [
        { url: "https://github.com/o/r/pull/12", batch: "", item: "", session: "s-1", dir: "/r", action: "fix-ci", checks: "red", status: "fixing", pid: 42 },
        { url: "https://github.com/o/r/pull/13", batch: "b1", item: "t2", dir: "/r", action: "none", checks: "green", status: "green" },
        { url: "https://github.com/o/r/pull/14", batch: "b1", item: "t3", dir: "/r", action: "rebase", checks: "red", status: "error", error: "gh: not logged in" },
      ],
    });
    expect(rows.map((r) => [r.from, r.session, r.label, r.status, r.tone, r.hint])).toEqual([
      ["session s-1", "s-1", "#12", "fixing", "run", "fix-ci · checks red · tend session pid 42"],
      ["b1/t2", undefined, "#13", "green", "done", "none · checks green"],
      ["b1/t3", undefined, "#14", "error", "fail", "gh: not logged in"],
    ]);
  });
  it("has no rows when nothing is tended", () => {
    expect(tendRows({ mode: "off", prs: [] })).toEqual([]);
  });
});
