/* FreeNet PWA: cache only public static shell. Never cache API, configs, health. */
const SHELL_CACHE = "freenet-v09-shell-v1";
const SHELL_ASSETS = [
  "/static/style.css",
  "/static/app.js",
  "/static/icon.svg",
  "/static/icon-180.png",
  "/static/icon-192.png",
  "/static/icon-512.png",
  "/manifest.webmanifest"
];
self.addEventListener("install", event => {
  event.waitUntil(caches.open(SHELL_CACHE).then(cache => cache.addAll(SHELL_ASSETS)).then(() => self.skipWaiting()));
});
self.addEventListener("activate", event => {
  event.waitUntil(caches.keys().then(keys => Promise.all(
    keys.filter(key => key.startsWith("freenet-v09-shell-") && key !== SHELL_CACHE).map(key => caches.delete(key))
  )).then(() => self.clients.claim()));
});
self.addEventListener("fetch", event => {
  const req = event.request;
  if (req.method !== "GET" || req.mode === "navigate") return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;
  if (!SHELL_ASSETS.includes(url.pathname)) return;
  event.respondWith(caches.match(req).then(hit => hit || fetch(req)));
});
