import { describe, expect, it } from "vitest";
import { Heights } from "./virt";

describe("Heights", () => {
  it("windows 10k rows around the scroll position with overscan", () => {
    const h = new Heights(20);
    h.resize(10000);
    expect(h.total()).toBe(200000);
    expect(h.window(100000, 400, 5)).toEqual({ from: 4995, to: 5025, top: 99900 });
  });
  it("uses measured heights", () => {
    const h = new Heights(10);
    h.resize(4);
    h.set(1, 50);
    expect(h.total()).toBe(80);
    expect(h.offset(2)).toBe(60);
    expect(h.window(55, 10, 0)).toEqual({ from: 1, to: 3, top: 10 });
  });
  it("clamps at the ends", () => {
    const h = new Heights(10);
    h.resize(3);
    expect(h.window(0, 1000, 2)).toEqual({ from: 0, to: 3, top: 0 });
    h.resize(0);
    expect(h.window(0, 100, 2)).toEqual({ from: 0, to: 0, top: 0 });
  });
});
