import { describe, expect, it } from "vitest";
import { THEMES, accentOf, themeOf } from "./theme";

describe("theme", () => {
  it("lists steel first, the same names as ui.theme", () => {
    expect(THEMES.map((t) => t.name)).toEqual(["steel", "frost", "plum"]);
  });
  it("reads ui.theme from GET /v1/settings, steel when unset or unknown", () => {
    expect(themeOf({ settings: [{ key: "ui.theme", value: "plum" }] })).toBe("plum");
    expect(themeOf({ settings: [{ key: "ui.theme", value: "neon" }] })).toBe("steel");
    expect(themeOf({ settings: [] })).toBe("steel");
    expect(themeOf(null)).toBe("steel");
  });
  it("steel needs no attribute; the others set data-accent", () => {
    expect(accentOf("steel")).toBeUndefined();
    expect(accentOf("frost")).toBe("frost");
  });
});
