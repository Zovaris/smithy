import { describe, expect, it } from "vitest";
import { plural, tail } from "./text";

describe("text", () => {
  it("pluralises a count like internal/text.Plural", () => {
    expect(plural(1, "saved run", "saved runs")).toBe("1 saved run");
    expect(plural(0, "saved run", "saved runs")).toBe("0 saved runs");
    expect(plural(3, "saved run", "saved runs")).toBe("3 saved runs");
  });
  it("keeps a log's last lines", () => {
    expect(tail("a\nb\nc\nd\n", 2)).toBe("c\nd");
  });
});
