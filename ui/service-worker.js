// Cache version is bumped whenever the precache contents change so the
// new install overwrites instead of layering on top of the old cache,
// and so the activate handler below can deterministically prune stale
// entries left over from previous versions.
const CACHE_NAME = 'gosspin-ui-v8';

self.addEventListener('install', event => {
  // Take control of all clients immediately so the new caching strategy
  // is active as soon as the SW installs, without waiting for tabs to close.
  self.skipWaiting();

  event.waitUntil(
    caches.open(CACHE_NAME).then(cache => {
      return cache.addAll([
        '/ui',
        '/ui/',
        '/ui/home',
        '/ui/home.js',
        '/ui/ghostboard',
        '/ui/assets/app.js',
        // Vendored browser libraries: the pages load Alpine, htmx and
        // Tailwind from the server, so an offline reload without these
        // entries would paint an unstyled, script-less page.
        '/ui/assets/vendor/alpine-3.17.5.min.js',
        '/ui/assets/vendor/alpine-collapse-3.17.5.min.js',
        '/ui/assets/vendor/htmx-1.9.10.min.js',
        '/ui/assets/vendor/tailwind-2.2.19.min.css',
        '/ui/assets/pwa/app-icon-192.png',
        '/ui/assets/pwa/app-icon-512.png',
        // Ghostboard PWA manifest and icons. The manifest is fetched
        // network-first (see the fetch handler), so this entry is only the
        // offline fallback for it.
        '/ui/assets/pwa/manifest-ghostboard.json',
        '/ui/assets/pwa/ghostboard-icon-192.png',
        '/ui/assets/pwa/ghostboard-icon-512.png',
        // Homepage theme background images (locally bundled, served by
        // the static asset router r.Static("/ui/assets", "./ui/assets")
        // in internal/api/route/ui_route.go). Pre-cached so each theme's
        // background loads instantly on first paint and works offline.
        '/ui/assets/themes/nord-bg.jpg',
        '/ui/assets/themes/nord-bg-1280.jpg',
        '/ui/assets/themes/nord-bg.webp',
        '/ui/assets/themes/nord-bg-1280.webp',
        '/ui/assets/themes/catppuccin-latte-bg.jpg',
        '/ui/assets/themes/catppuccin-latte-bg-1280.jpg',
        '/ui/assets/themes/catppuccin-latte-bg.webp',
        '/ui/assets/themes/catppuccin-latte-bg-1280.webp',
        '/ui/assets/themes/night-stars-bg.jpg',
        '/ui/assets/themes/night-stars-bg-1280.jpg',
        '/ui/assets/themes/night-stars-bg.webp',
        '/ui/assets/themes/night-stars-bg-1280.webp'
      ]);
    })
  );
});

// Delete any cache left over from a previous SW version so we don't
// accumulate dead caches (and serve stale precached entries) across
// future deployments. Must run AFTER the new SW has installed.
self.addEventListener('activate', event => {
  event.waitUntil(
    caches.keys().then(keys =>
      Promise.all(
        keys.filter(key => key !== CACHE_NAME).map(key => caches.delete(key))
      )
    ).then(() => self.clients.claim())
  );
});

// Network-first strategy for HTML pages and JavaScript bundles so that
// deployments are picked up immediately without requiring a cache-busting
// query parameter or service-worker version bump. Everything else (static
// assets, theme images, etc.) keeps the cache-first strategy for speed.
self.addEventListener('fetch', event => {
  const url = new URL(event.request.url);
  const isGhostboardPage = url.pathname === '/ui/ghostboard' ||
                           url.pathname.startsWith('/ui/ghostboard/');
  const isHTMLorJS = url.pathname === '/ui' ||
                     url.pathname === '/ui/home' ||
                     url.pathname === '/ui/' ||
                     isGhostboardPage ||
                     url.pathname.endsWith('.js');

  if (isHTMLorJS) {
    event.respondWith(
      fetch(event.request)
        .then(response => {
          // Every /ui/ghostboard/<board> path serves the same page: keep a
          // single copy under the canonical path instead of one entry per board
          // name, which would grow the cache without bound.
          if (isGhostboardPage) {
            const cloned = response.clone();
            event.waitUntil(
              caches.open(CACHE_NAME).then(cache => cache.put('/ui/ghostboard', cloned))
            );
            return response;
          }
          // Clone the response before putting it in cache (the body can
          // only be consumed once). Use waitUntil so the cache write
          // completes even if the fetch handler terminates early.
          const cloned = response.clone();
          event.waitUntil(
            caches.open(CACHE_NAME).then(cache => cache.put(event.request, cloned))
          );
          return response;
        })
        .catch(() => {
          if (isGhostboardPage) {
            return caches.match('/ui/ghostboard', { ignoreSearch: true });
          }
          // ignoreSearch: true is critical — the HTML page injects a
          // ?v=<hash> cache-busting query parameter on every script tag.
          // Without it, the offline fallback would look for
          // /ui/home.js?v=abc123 in cache, miss the pre-cached
          // /ui/home.js, and serve nothing.
          return caches.match(event.request, { ignoreSearch: true });
        })
    );
    return;
  }

  // Manifests change with the app (name, icons, scope) and the router serves
  // them with no-cache on purpose, so they are fetched network-first: the
  // pre-cached copy is only the offline fallback.
  if (url.pathname.includes('manifest')) {
    event.respondWith(
      fetch(event.request)
        .then(response => {
          const cloned = response.clone();
          return caches.open(CACHE_NAME)
            .then(cache => cache.put(event.request, cloned))
            .then(() => response);
        })
        .catch(() => caches.match(event.request, { ignoreSearch: true }))
    );
    return;
  }

  // Cache-first for everything else (icons, theme images, vendored
  // libraries, etc.). ignoreSearch is what lets the entries precached
  // without a query string match the ?v=<hash> URLs the pages use, so a
  // vendored library is available offline on the first load too.
  event.respondWith(
    caches.match(event.request, { ignoreSearch: true }).then(response => {
      return response || fetch(event.request);
    })
  );
});
