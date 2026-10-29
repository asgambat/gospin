# Frontend Guide

The whole UI lives in `ui/` and is hand-written HTML and JavaScript served directly by the Go
binary (`internal/api/route/ui_route.go`). There is **no Node toolchain**: no `package.json`, no
bundler, no transpiler, no `node_modules` and no JavaScript test runner.

## Stack and constraints

- Write plain JavaScript (ES2020+) and HTML only. No TypeScript, JSX/TSX, React, Vue, Svelte or any
  compile step.
- Alpine.js 3, htmx 1.9.10 and Tailwind 2.2.19 are loaded from the CDNs pinned in the HTML. Adding
  any other library requires maintainer approval — everything under `ui/` also ships to offline PWA
  users.
- Style with Tailwind utility classes and the existing `theme-<name>` body classes; avoid inline
  styles except for values driven by configuration (e.g. font size from `settings`).

## Files and routes

| File | Served at |
| ---- | --------- |
| `ui/index.html` → `ui/assets/app.js` | `/ui` (containers, groups, schedules dashboard) |
| `ui/home.html` → `ui/home.js` | `/ui/home` (personal dashboard, and the SPA fallback for unhandled `/ui/*`) |
| `ui/ghostboard.html` → `ui/assets/ghostboard.js` | `/ui/ghostboard[/:board]` (shared text board) |
| `ui/templates/waiting.html` | waiting page on the second server port (`{{CONTAINER_NAME}}`, `{{REDIRECT_URL}}`) |
| `ui/assets/**` | `/ui/assets/**` (shared runtime assets, theme backgrounds, PWA manifests) |
| `ui/service-worker.js` | `/ui/service-worker.js` (one hour cache, `Service-Worker-Allowed: /`) |
| `ui/sw-register.js` | `/ui/sw-register.js` (one hour cache) |
| `ui/assets/vite.ico` | `/favicon.ico` (one day cache) |
| - | `/` redirects to `/ui` |

- The Go server appends a content hash (`?v=...`) to the `app.js`, `home.js` and `ghostboard.js`
  script tags: keep the script tags pointing at the served paths.
- `ui/ghostboard.html` renders the board and, for a board listed in `ghostboard.private_boards`,
  a PIN form in its place. The page loads the board state before opening the `EventSource`, so a
  locked board never leaves a stream retrying against a `401`; when a save answers `401` the form
  comes back and the locally typed text is kept until the unlock writes it.
- `ui/assets/app.js` and `ui/home.js` are served as-is (single-line, minified). Edit them directly,
  keep dead code out, and do not rename them without updating the cache-buster paths in
  `internal/api/route/ui_route.go`.

## Alpine.js components

1. **Code structure**: keep each component a single object factory
   (`function app() { return { state, methods } }`), defined in `ui/assets/app.js` for the
   dashboard and `ui/home.js` for the homepage; group related helpers as methods, not globals.
2. **State**: keep all state inside the returned object; `init()` wires timers, listeners and the
   first fetch. Do not use module-level mutable variables.
3. **API calls**: `fetch` against the Go endpoints — `this.apiBase` for the dashboard routes and
   absolute paths (`/homepage`, `/homepage/action`, `/runtime/system-stats`) for the homepage.
   Always check `response.ok`, surface failures through the existing `showError`/`showSuccess`
   helpers, and `encodeURIComponent` every path segment.
4. **Configuration over magic numbers**: intervals come from the server
   (`settings.pollingIntervalSeconds`, `settings.statsPollingIntervalSeconds`, `/configuration`),
   never hardcoded.
5. **Persistence**: user preferences use the existing keys (`homepage-theme`,
   `homepage-collapsed-groups`, `gosspin.containerFilters`, `gosspin.ghostboard.theme`); wrap
   `JSON.parse` in `try/catch` and fall back to a safe default on failure.
6. **Cleanup**: clear timers and listeners before re-registering them (`clearInterval` before
   `setInterval`) and guard re-entrant actions with the existing `isXxx` flags.
7. **Escaping**: render server data with `x-text`; never `x-html` with data coming from the API
   (XSS).
8. **Theme names**: `applyTheme` accepts only the names in the `themes` array and falls back to
   `auto` for anything else, and the server normalizes `settings.theme` through
   `config.NormalizeHomepageTheme`. A theme therefore has to be added in lockstep in three places:
   the `body.theme-<name>` variables in `ui/home.html`, the `themes` array in `ui/home.js` and
   `config.HomepageThemes` in `internal/config/homepage_config.go`. A name missing from the CSS
   renders an unstyled page; one missing from the Go list is silently replaced by `auto`.

## PWA and service worker

1. `ui/service-worker.js` precaches the app shell, the pages, the theme backgrounds and the icons.
   Bump `CACHE_NAME` (`gosspin-ui-vN`) in the same change that adds, removes or renames a precached
   asset, and keep the list in sync with the files that exist.
2. `ui/sw-register.js` registers the worker with `scope: '/'` and is included by every page, so a
   device that only opens one page still gets it. Do not add inline registrations or per-page
   variants.
3. Assets are served with `Cache-Control: public, max-age=31536000`, except the PWA manifests
   (`no-cache`). Do not weaken these headers without updating the worker strategy: it fetches
   manifests network-first and keeps the precached copy only as the offline fallback, so a manifest
   edit needs no `CACHE_NAME` bump.
4. Keep `ui/assets/pwa/*.json`, the icons and the `/ui`, `/ui/home` and `/ui/ghostboard` scopes
   consistent when adding pages or themes.

## Accessibility and performance

- Semantic HTML first; ARIA complements it, never substitutes it. Prefer native controls, clear
  labels, visible focus, sufficient contrast and keyboard operability.
- Keep the payload small: the HTML and the JavaScript files ship on every first load, so prefer
  extending the existing Alpine objects over adding libraries.
- Avoid DOM churn: update state instead of recreating large sections, and keep `x-for` keys stable.
- Reuse the configured polling intervals, do not stack intervals and stop timers on hidden pages
  where the code already does so.
- Reference the bundled theme images through absolute `/ui/assets/...` paths (`.webp` preferred) so
  both the router and the service worker can cache them.
- Remove debugging `console.log` noise; keep `console.warn`/`console.error` for real failures.

## Testing and verification

- There is no JavaScript test runner: the UI is verified through the Go tests in
  `internal/api/route/ui_route_test.go` with `httptest`. When you change what a UI route serves
  (status code, content type, cache headers, injected cache-buster, fallback behaviour), extend
  those Go tests.
- Manual check: `task run`, then open `/ui` and `/ui/home`; verify the console is free of errors,
  that polling updates the page and that a hard reload after a service-worker bump still works.
  Exercise the modal or control you touched and report anything you could not verify.
- Screenshots: after a visual change, `task screenshots` regenerates `docs/screenshots/` from a
  demo instance instead of a real one. It needs Node and installs Playwright under `.build/`; the
  fixtures it serves live in `scripts/screenshots/fixtures/`.
- Gates before a PR: `go vet ./...`, `golangci-lint run --concurrency=1 ./...`, `go test ./...`
  (at minimum `go test ./internal/api/route` for UI-only changes).
