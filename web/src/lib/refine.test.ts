import { describe, expect, it } from "vitest";
import { refined } from "./refine";
import type { Item } from "./store";

const reply = "Here it is.\n\n```refined\n# Goal\nShip the thing.\n```\n\nCopy it or start a new chat.";
const agent = (text: string): Item => ({ kind: "agent", at: "", parts: [{ kind: "text", text }] });

describe("refine", () => {
  it("takes the refined block the last agent reply ends with", () => {
    expect(refined([agent(reply)])).toBe("# Goal\nShip the thing.");
  });
  it("is empty for a plain code block or once you replied", () => {
    expect(refined([agent("```go\nx\n```")])).toBe("");
    expect(refined([agent(reply), { kind: "you", text: "thanks", at: "" }])).toBe("");
  });
});
