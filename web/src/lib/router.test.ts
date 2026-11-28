import { describe, expect, it } from "vitest";
import { href, PAGES, route } from "./router";

describe("route", () => {
  it("maps a hash to a view", () => {
    expect(route("#/s/s-ab12")).toEqual({ name: "session", id: "s-ab12" });
    expect(route("#/board")).toEqual({ name: "board" });
    expect(route("#/setup")).toEqual({ name: "setup" });
    expect(route("#/new")).toEqual({ name: "new" });
  });
  it("keeps old session links working", () => {
    expect(route("#/sessions?s=s-1")).toEqual({ name: "session", id: "s-1" });
    expect(route("#/sessions")).toEqual({ name: "new" });
  });
  it("defaults to a new session for empty or unknown hashes", () => {
    expect(route("")).toEqual({ name: "new" });
    expect(route("#/nope")).toEqual({ name: "new" });
  });
  it("round-trips through href", () => {
    for (const v of [{ name: "session", id: "s-1/x" } as const, { name: "agents" } as const]) expect(route(href(v))).toEqual(v);
  });
  it("lists the pages in shortcut order", () => {
    expect(PAGES.map((p) => p.key)).toEqual(["2", "3", "4", "5", "6", "7"]);
  });
});
