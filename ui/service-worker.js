// Cache version is bumped whenever the precache contents change so the
// new install overwrites instead of layering on top of the old cache,
// and so the activate handler below can deterministically prune stale
// entries left over from previous versions.
const CACHE_NAME = 'gosspin-ui-v4';

self.addEventListener('install', event => {
  event.waitUntil(
    caches.open(CACHE_NAME).then(cache => {
      return cache.addAll([
        '/ui/',
        '/ui/home',
        '/ui/home.js',
        '/ui/assets/app.js',
        '/ui/assets/pwa/app-icon-192.png',
        '/ui/assets/pwa/app-icon-512.png',
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
    )
  );
});

self.addEventListener('fetch', event => {
  event.respondWith(
    caches.match(event.request).then(response => {
      return response || fetch(event.request);
    })
  );
});
