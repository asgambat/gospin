// Registers the application service worker for every UI page. Keeping the
// registration in one shared script gives the main UI, the homepage and the
// Ghostboard a single registration instead of one per page: the scope is "/"
// so the same worker controls all of them, and a device that lands directly on
// /ui/ghostboard still gets the offline shell and the cached assets.
if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/ui/service-worker.js', { scope: '/' }).catch((error) => {
      console.warn('ServiceWorker registration failed:', error);
    });
  });
}
