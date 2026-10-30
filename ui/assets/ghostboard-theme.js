// Applies the saved ghostboard theme before the page paints, so a dark-mode
// board does not flash light. It lives in its own file instead of an inline
// <script> so the Content-Security-Policy can refuse inline scripts on every
// page; it still loads synchronously in the head, at the same point the inline
// snippet used to run.
(function () {
  try {
    var saved = localStorage.getItem('gosspin.ghostboard.theme');
    var dark = saved === 'dark' || (!saved && window.matchMedia('(prefers-color-scheme: dark)').matches);
    if (dark) {
      document.documentElement.classList.add('gb-dark');
    }
  } catch (error) {
    // Storage can be unavailable in private mode; keep the light theme.
  }
})();
