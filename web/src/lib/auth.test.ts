import { describe, expect, it } from "vitest";
import { authHeaders, serverPath, takeToken } from "./auth";

function mem() {
  const m = new Map<string, string>();
  return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), removeItem: (k: string) => void m.delete(k) };
}

describe("takeToken", () => {
  it("reads the token from the fragment, stores it and returns the hash without it", () => {
    const s = mem();
    const r = takeToken("#token=abc123&/decisions", s);
    expect(r.token).toBe("abc123");
    expect(r.hash).toBe("#/decisions");
    expect(s.getItem("smithy.token")).toBe("abc123");
  });
  it("falls back to the stored token when the fragment has none", () => {
    const s = mem();
    s.setItem("smithy.token", "kept");
    expect(takeToken("#/board", s)).toEqual({ token: "kept", hash: "#/board" });
  });
  it("returns no token when there is none (Tailscale login carries auth)", () => {
    expect(takeToken("", mem())).toEqual({ token: "", hash: "" });
  });
});

describe("authHeaders", () => {
  it("sends a bearer only when there is a token", () => {
    expect(authHeaders("t")).toEqual({ Authorization: "Bearer t" });
    expect(authHeaders("")).toEqual({});
  });
});

describe("serverPath", () => {
  it("leaves paths alone on this machine", () => {
    expect(serverPath("/v1/state", "")).toBe("/v1/state");
  });
  it("sends a picked server's API through this daemon", () => {
    expect(serverPath("/v1/events", "home")).toBe("/v1/servers/home/v1/events");
    expect(serverPath("/v1/sessions/s1/stop", "mac mini")).toBe("/v1/servers/mac%20mini/v1/sessions/s1/stop");
  });
  it("never proxies the server list itself or non-API paths", () => {
    expect(serverPath("/v1/servers", "home")).toBe("/v1/servers");
    expect(serverPath("/assets/x.js", "home")).toBe("/assets/x.js");
  });
});
