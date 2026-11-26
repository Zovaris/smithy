// Offline shell only: the app page and its hashed assets. API calls (/v1/), anything with a
// query string (tokens), non-GET requests and other origins go straight to the network.
const CACHE = "smithy-shell-v1";
const SHELL = "/";
const STATIC = /^\/(assets\/[^/]+|favicon\.svg|icon-\d+\.png|apple-touch-icon\.png)$/;

self.addEventListener("install", (e) => {
  self.skipWaiting();
  e.waitUntil(caches.open(CACHE).then((c) => c.add(SHELL)));
});

self.addEventListener("activate", (e) => {
  e.waitUntil(
    caches
      .keys()
      .then((ks) => Promise.all(ks.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  );
});

self.addEventListener("fetch", (e) => {
  const r = e.request;
  if (r.method !== "GET") return;
  const u = new URL(r.url);
  if (u.origin !== self.location.origin || u.pathname.startsWith("/v1/")) return;
  if (r.mode === "navigate") {
    e.respondWith(
      fetch(r)
        .then((res) => {
          if (res.ok && !u.search) {
            const copy = res.clone();
            caches.open(CACHE).then((c) => c.put(SHELL, copy));
          }
          return res;
        })
        .catch(() => caches.match(SHELL).then((m) => m || Response.error())),
    );
    return;
  }
  if (u.search || !STATIC.test(u.pathname)) return;
  e.respondWith(
    caches.match(r).then(
      (m) =>
        m ||
        fetch(r).then((res) => {
          if (res.ok) {
            const copy = res.clone();
            caches.open(CACHE).then((c) => c.put(r, copy));
          }
          return res;
        }),
    ),
  );
});
