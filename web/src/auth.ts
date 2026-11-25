// The daemon's token arrives once in the URL fragment (`smithy web` prints it); it never reaches
// a server log or Referer. It is kept in sessionStorage and stripped from the address bar.
// Behind tailscale serve there is no token: the proxy's login header authorizes each request.

type Store = Pick<Storage, "getItem" | "setItem">;
const KEY = "smithy.token";

export function takeToken(hash: string, store: Store): { token: string; hash: string } {
  const parts = hash.replace(/^#/, "").split("&").filter(Boolean);
  let token = "";
  const rest: string[] = [];
  for (const p of parts) {
    if (p.startsWith("token=")) token = decodeURIComponent(p.slice(6));
    else rest.push(p);
  }
  if (token) store.setItem(KEY, token);
  else token = store.getItem(KEY) ?? "";
  return { token, hash: rest.length ? "#" + rest.join("&") : "" };
}

export function authHeaders(token: string): Record<string, string> {
  return token ? { Authorization: `Bearer ${token}` } : {};
}

let current = "";

export function initAuth(): void {
  let store: Store;
  try {
    store = sessionStorage;
  } catch {
    const m = new Map<string, string>();
    store = { getItem: (k) => m.get(k) ?? null, setItem: (k, v) => void m.set(k, v) };
  }
  const r = takeToken(location.hash, store);
  current = r.token;
  if (r.hash !== location.hash) history.replaceState(null, "", location.pathname + location.search + r.hash);
}

export function api(path: string, init: RequestInit = {}): Promise<Response> {
  return fetch(path, { ...init, headers: { ...authHeaders(current), ...(init.headers as Record<string, string>) } });
}
