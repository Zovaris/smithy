import { describe, expect, it } from "vitest";

// Read as text: Vitest blanks CSS imports, and the web has no Node types.
const fs = "node:fs";
const { readFileSync } = (await import(fs)) as { readFileSync: (u: URL, e: "utf8") => string };
const app = readFileSync(new URL("./app.css", import.meta.url), "utf8");
const temper = readFileSync(new URL("./theme/temper.css", import.meta.url), "utf8");
const rule = (sel: string) => app.match(new RegExp(`(?:^|[}\\s])${sel.replace(/[.]/g, "\\.")}\\s*\\{([^}]*)\\}`, "m"))?.[1] ?? "";
const px = (block: string, prop: string) => Number(block.match(new RegExp(`(?:^|[;\\s])${prop}:\\s*(\\d+)px`))?.[1]);

describe("flat design", () => {
  it("sets every Sephiro radius token to 0", () => {
    const tokens = [...temper.matchAll(/--sph-radius-[a-z]+:\s*([^;]+);/g)].map((m) => m[1].trim());
    expect(tokens.length).toBeGreaterThanOrEqual(3);
    expect(tokens.every((v) => v === "0")).toBe(true);
  });
  it("has no rounded corner in the app's own CSS", () => {
    const radii = [...app.matchAll(/border-radius:\s*([^;}]+)/g)].map((m) => m[1].trim());
    expect(radii.filter((v) => v !== "0")).toEqual([]);
  });
  it("gives the stop/send button, chips and small buttons one control height", () => {
    const h = px(rule(".tag"), "height");
    expect(h).toBeGreaterThan(0);
    for (const sel of [".send", ".pill", ".delta", ".btn.sm", ".icon.s", ".meta-bar .btn"]) {
      const b = rule(sel);
      expect([sel, px(b, "height")]).toEqual([sel, h]);
    }
  });
  it("widens the chat column past 720px", () => {
    for (const sel of [".conv", ".composer", ".hello"]) expect(px(rule(sel), "max-width")).toBeGreaterThanOrEqual(880);
  });
  it("leaves the empty palette's highlight to Sephiro (Zovaris/Sephiro#37)", () => {
    const pal = readFileSync(new URL("../node_modules/@zovaris/sephiro/dist/css/command-palette.css", import.meta.url), "utf8");
    expect(pal).toMatch(/\.sph-palette__indicator\{[^}]*opacity:0/);
    expect(app).not.toMatch(/sph-palette__/);
  });
});
