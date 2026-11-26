import { describe, expect, it } from "vitest";
import html from "../index.html?raw";
import manifest from "../public/manifest.webmanifest?raw";
import sw from "../public/sw.js?raw";

const PUBLIC = Object.keys(import.meta.glob("../public/*"));

type Listener = (e: any) => void;
function loadSW() {
  const on: Record<string, Listener> = {};
  const self = { addEventListener: (t: string, f: Listener) => (on[t] = f), location: { origin: "https://h" }, skipWaiting: () => {}, clients: { claim: () => {} } };
  const pending = () => new Promise(() => {});
  const caches = { match: pending, open: pending, keys: pending };
  new Function("self", "caches", "fetch", sw)(self, caches, pending);
  const handles = (url: string, method = "GET", mode = "cors") => {
    let hit = false;
    on.fetch({ request: { url, method, mode }, respondWith: () => (hit = true) });
    return hit;
  };
  return { on, handles };
}

describe("service worker", () => {
  it("serves the shell offline but never touches API calls, tokens or other origins", () => {
    const { handles } = loadSW();
    expect(handles("https://h/", "GET", "navigate")).toBe(true);
    expect(handles("https://h/board", "GET", "navigate")).toBe(true);
    expect(handles("https://h/assets/index-abc.js")).toBe(true);
    expect(handles("https://h/v1/state")).toBe(false);
    expect(handles("https://h/v1/sessions?token=x")).toBe(false);
    expect(handles("https://h/assets/x.js?token=x")).toBe(false);
    expect(handles("https://h/assets/x.js", "POST")).toBe(false);
    expect(handles("https://other/assets/x.js")).toBe(false);
  });
});

describe("manifest and metadata", () => {
  it("makes the web installable", () => {
    const m = JSON.parse(manifest);
    expect(m.name).toBe("smithy");
    expect(m.start_url).toBe("/");
    expect(m.display).toBe("standalone");
    const sizes = m.icons.filter((i: any) => i.type === "image/png").map((i: any) => i.sizes);
    expect(sizes).toEqual(expect.arrayContaining(["192x192", "512x512"]));
    for (const i of m.icons) expect(PUBLIC).toContain("../public" + i.src);
  });
  it("index.html links icon, manifest and iOS tags", () => {
    const h = html;
    for (const s of ['rel="icon"', 'rel="manifest"', 'rel="apple-touch-icon"', 'name="description"', 'property="og:title"', 'name="apple-mobile-web-app-capable"', 'name="theme-color"'])
      expect(h).toContain(s);
  });
});
