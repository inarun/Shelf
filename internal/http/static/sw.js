// Shelf service worker — minimal app-shell cache for offline-friendly
// reloads of the library and book-detail shells.
//
// Security / correctness rules:
//   * Same-origin only. The fetch handler bails out for any non-GET or
//     cross-origin request and lets the network handle it normally. This
//     keeps POST/PATCH (rating, status, import) strictly uncached and
//     preserves CSRF semantics — the SW never touches state-changing
//     traffic.
//   * API and HTML documents are network-first. A stale cached copy is
//     returned only when the network is unreachable, so fresh frontmatter
//     and timeline data always win under normal conditions.
//   * Static assets (/static/*) are cache-first — the content hash is
//     effectively the binary's build; on a new binary, CACHE_VERSION
//     bumps and old caches are deleted in `activate`.
//   * The service worker does not cache responses that carry a
//     Set-Cookie header or a CSRF token, by only caching GETs of /static
//     and SSR HTML (which the server already sets Cache-Control on).
//   * Scope is origin root (/) because this file is served from /sw.js.

"use strict";

// Bump CACHE_VERSION on every static-asset change so returning clients
// install the new bundle instead of serving the old cache-first copy.
const CACHE_VERSION = "shelf-v6";
const STATIC_PREFIX = "/static/";

self.addEventListener("install", (event) => {
  // No pre-cache — the first real fetch warms the cache. Keeps install
  // fast and avoids caching stale files if the binary restarts mid-install.
  self.skipWaiting();
  event.waitUntil(Promise.resolve());
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    (async () => {
      const names = await caches.keys();
      await Promise.all(
        names
          .filter((n) => n !== CACHE_VERSION)
          .map((n) => caches.delete(n)),
      );
      await self.clients.claim();
    })(),
  );
});

self.addEventListener("fetch", (event) => {
  const req = event.request;
  if (req.method !== "GET") return;

  let url;
  try {
    url = new URL(req.url);
  } catch (_) {
    return;
  }
  if (url.origin !== self.location.origin) return;

  // Never cache API or import traffic.
  if (url.pathname.startsWith("/api/")) return;

  if (url.pathname.startsWith(STATIC_PREFIX) ||
      url.pathname === "/manifest.webmanifest" ||
      url.pathname === "/favicon.svg") {
    event.respondWith(cacheFirst(req));
    return;
  }

  // SSR pages — network first with a cached fallback for offline.
  if (req.destination === "document" ||
      url.pathname === "/" ||
      url.pathname === "/library" ||
      url.pathname.startsWith("/books/") ||
      url.pathname === "/import") {
    event.respondWith(networkFirst(req));
  }
});

async function cacheFirst(req) {
  const cache = await caches.open(CACHE_VERSION);
  const hit = await cache.match(req);
  if (hit) return hit;
  const resp = await fetch(req);
  if (resp && resp.ok && resp.type === "basic") {
    cache.put(req, resp.clone());
  }
  return resp;
}

async function networkFirst(req) {
  const cache = await caches.open(CACHE_VERSION);
  try {
    const resp = await fetch(req);
    if (resp && resp.ok && resp.type === "basic") {
      cache.put(req, resp.clone());
    }
    return resp;
  } catch (err) {
    const hit = await cache.match(req);
    if (hit) return hit;
    throw err;
  }
}
