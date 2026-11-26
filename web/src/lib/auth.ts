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
let server = "";
const SERVER = "smithy.server";

// serverPath sends a picked server's API through this daemon's /v1/servers/<name> proxy, which
// adds that server's token; "" is this machine. The server list itself is always this machine's.
export function serverPath(path: string, srv: string): string {
  if (!srv || !path.startsWith("/v1/") || path === "/v1/servers" || path.startsWith("/v1/servers/")) return path;
  return `/v1/servers/${encodeURIComponent(srv)}${path}`;
}

export function currentServer(): string {
  return server;
}

// pickServer remembers the server for this tab and reloads into it ("" is this machine).
export function pickServer(srv: string, hash = "#/new"): void {
  try {
    if (srv) sessionStorage.setItem(SERVER, srv);
    else sessionStorage.removeItem(SERVER);
  } catch {
    /* no storage: the pick lasts until the reload */
  }
  location.hash = hash;
  location.reload();
}

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
  server = store.getItem(SERVER) ?? "";
  if (r.hash !== location.hash) history.replaceState(null, "", location.pathname + location.search + r.hash);
}

export function api(path: string, init: RequestInit = {}): Promise<Response> {
  return fetch(serverPath(path, server), { ...init, headers: { ...authHeaders(current), ...(init.headers as Record<string, string>) } });
}
