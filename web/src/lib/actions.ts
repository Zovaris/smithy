// The daemon calls behind the TUI commands the web now has: a session's model and effort, `!cmd`,
// pasted images, /ask, settings, API keys, serve, doctor and saved servers.
import { api } from "./auth";
import type { SettingRow } from "./parity";
import type { Sess } from "./store";

async function call(method: string, path: string, body?: unknown): Promise<any> {
  const r = await api(path, body === undefined ? { method } : { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || `HTTP ${r.status}`);
  return j;
}

const sid = (id: string) => "/v1/sessions/" + encodeURIComponent(id);

export const setSessionModel = (id: string, model: string, effort: string): Promise<Sess> => call("PUT", sid(id) + "/model", { model, effort });
export const runShell = (id: string, cmd: string): Promise<{ output: string; code: number; error?: string }> => call("POST", sid(id) + "/shell", { cmd });

export async function uploadImage(f: Blob): Promise<string> {
  if (f.size > 32 << 20) throw new Error("image over 32 MiB");
  const b = new Uint8Array(await f.arrayBuffer());
  let s = "";
  for (let i = 0; i < b.length; i += 0x8000) s += String.fromCharCode(...b.subarray(i, i + 0x8000));
  return (await call("POST", "/v1/images", { data: btoa(s) })).path;
}

// sampleAsk is /ask: a question pinned so you can see and try how decisions work.
export const sampleAsk = (q: string) =>
  call("POST", "/v1/asks", { id: "ask:web-" + Date.now().toString(36), question: q || "Ship the sample change?", options: ["yes", "no"] });

export type Settings = { settings: SettingRow[]; keys: { service: string; masked: string }[]; serve: boolean; prompt?: { path: string; active: boolean } };
export const loadSettings = (): Promise<Settings> => call("GET", "/v1/settings");
export const setSetting = (k: string, v: string) => call("PUT", "/v1/settings/" + encodeURIComponent(k), { value: v });
export const unsetSetting = (k: string) => call("DELETE", "/v1/settings/" + encodeURIComponent(k));
export const setKey = (svc: string, key: string) => call("PUT", "/v1/keys/" + encodeURIComponent(svc), { key });
export const removeKey = (svc: string) => call("DELETE", "/v1/keys/" + encodeURIComponent(svc));
export const setServe = (on: boolean) => call("PUT", "/v1/serve", { on });
export const runDoctor = (): Promise<{ lines: string[] }> => call("GET", "/v1/doctor");
export const addServer = (name: string, url: string, code: string) => call("POST", "/v1/servers", { name, url, code });
export const removeServer = (name: string) => call("DELETE", "/v1/servers/" + encodeURIComponent(name));
