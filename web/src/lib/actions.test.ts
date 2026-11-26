import { afterEach, describe, expect, it, vi } from "vitest";
import { removeKey, unsetSetting } from "./actions";

// daemonLike answers as the daemon's sameOrigin does: a body must be JSON, a body-less DELETE needs no type.
function daemonLike(calls: { method: string; url: string }[]) {
  return vi.fn(async (url: string, init: RequestInit = {}) => {
    calls.push({ method: init.method ?? "GET", url });
    const ct = (init.headers as Record<string, string> | undefined)?.["Content-Type"] ?? "";
    const bodyless = init.method === "DELETE" && init.body == null;
    if (ct !== "application/json" && !bodyless) return new Response(JSON.stringify({ error: "Content-Type must be application/json" }), { status: 415 });
    return new Response(JSON.stringify({ found: true }), { status: 200 });
  });
}

afterEach(() => vi.unstubAllGlobals());

describe("removing a key and resetting a setting", () => {
  it("send a body-less DELETE the daemon accepts", async () => {
    const calls: { method: string; url: string }[] = [];
    vi.stubGlobal("fetch", daemonLike(calls));
    await expect(removeKey("linear")).resolves.toEqual({ found: true });
    await expect(unsetSetting("linear.team")).resolves.toEqual({ found: true });
    expect(calls).toEqual([
      { method: "DELETE", url: "/v1/keys/linear" },
      { method: "DELETE", url: "/v1/settings/linear.team" },
    ]);
  });
});
