import { describe, expect, it } from "vitest";
import { issueHint, linkSummary, prHint, type Links } from "./links";
import { blank, fold } from "./store";

const links: Links = {
  session: "s-1",
  prs: [{ url: "https://github.com/o/r/pull/7", number: 7, title: "Live links", state: "OPEN", checks: "green", review: "APPROVED", mergeable: "MERGEABLE", additions: 120, deletions: 8 }],
  issues: [
    { kind: "linear", id: "TK-112", title: "Live PR links", url: "https://linear.app/i/TK-112", status: "In Progress", assignee: "Ana", milestone: "v0.26" },
    { kind: "github", id: "#12", title: "Bug", url: "https://github.com/o/r/issues/12", status: "OPEN" },
  ],
  checked: "2026-11-27T00:00:00Z",
};

describe("links, the same lines as the TUI's /links", () => {
  it("says a PR's state and lines first (they must not be cut), then checks, review, mergeable", () => {
    expect(prHint(links.prs[0])).toBe("OPEN · +120 −8 · checks green · approved · mergeable");
    expect(prHint({ ...links.prs[0], draft: true, checks: undefined, review: "CHANGES_REQUESTED", mergeable: undefined })).toBe("OPEN · +120 −8 · draft · changes requested");
  });
  it("says an issue's status, assignee and milestone", () => {
    expect(issueHint(links.issues[0])).toBe("In Progress · Ana · v0.26");
    expect(issueHint(links.issues[1])).toBe("OPEN");
  });
  it("sums up a session's links in one line like the agents panel", () => {
    expect(linkSummary(links)).toBe("PR #7 green +120 −8 · TK-112 In Progress · #12 OPEN");
    expect(linkSummary(undefined)).toBe("");
  });
  it("folds a pushed session_links event into the store, the latest winning", () => {
    let s = fold(blank(), { seq: 1, kind: "session_started", session: "s-1", time: "t", data: { id: "s-1", dir: "/r", title: "x", agent: "claude", status: "done" } } as any);
    s = fold(s, { seq: 2, kind: "session_links", session: "s-1", time: "t", data: links } as any);
    expect(s.links!["s-1"].prs[0].number).toBe(7);
    s = fold(s, { seq: 3, kind: "session_links", session: "s-1", time: "t", data: { ...links, prs: [] } } as any);
    expect(s.links!["s-1"].prs).toEqual([]);
  });
});
