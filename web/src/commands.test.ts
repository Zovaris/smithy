import { describe, expect, it } from "vitest";
import { action, suggest } from "./commands";

describe("commands", () => {
  it("suggests actions and skills for a bare /word, prefix matches first", () => {
    expect(suggest("/st", ["smelt", "tend"]).map((c) => c.name)).toEqual(["stop"]);
    expect(suggest("/s", ["smelt", "scout"]).map((c) => c.name)).toEqual(["stop", "settings", "smelt", "scout", "agents", "decisions"]);
    expect(suggest("/", []).length).toBe(7);
  });
  it("stops suggesting once the message goes on", () => {
    expect(suggest("/smelt fix", ["smelt"])).toEqual([]);
    expect(suggest("hello", ["smelt"])).toEqual([]);
  });
  it("runs only a message that is one smithy command", () => {
    expect(action(" /stop ")).toBe("stop");
    expect(action("/smelt fix ci")).toBe("");
    expect(action("/smelt")).toBe("");
    expect(action("stop")).toBe("");
  });
});
