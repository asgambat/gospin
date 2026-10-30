# Frontend Guide

The whole UI lives in `ui/` and is hand-written HTML and JavaScript served directly by the Go
binary (`internal/api/route/ui_route.go`). There is **no Node toolchain**: no `package.json`, no
bundler, no transpiler, no `node_modules` and no JavaScript test runner.

## Stack and constraints

- Write plain JavaScript (ES2020+) and HTML only. No TypeScript, JSX/TSX, React, Vue, Svelte or any
  compile step.
- Alpine.js 3.17.5, htmx 1.9.10 and Tailwind 2.2.19 are **vendored** under `ui/assets/vendor/`
  and served by the Go binary: no page may load a script, stylesheet or font from a CDN, because
  the UI has to work on a LAN without internet access. Adding any other library requires
  maintainer approval and the same treatment.
- Style with Tailwind utility classes and the existing `theme-<name>` body classes; avoid inline
  styles except for values driven by configuration (e.g. font size from `settings`).

## Files and routes

| File | Served at |
| ---- | --------- |
| `ui/index.html` → `ui/assets/app.js` | `/ui` (containers, groups, schedules dashboard) |
| `ui/home.html` → `ui/home.js` | `/ui/home` (personal dashboard, and the SPA fallback for unhandled `/ui/*`) |
| `ui/ghostboard.html` → `ui/assets/ghostboard.js` | `/ui/ghostboard[/:board]` (shared text board and its files) |
| `ui/sealed.html` → `ui/assets/sealed.js` | `/ui/sealed[/:board]` (create and reveal a sealed board; own CSP, no service worker) |
| `ui/assets/sealed-vector.json` | `/ui/assets/sealed-vector.json` (known-answer vector shared with `internal/sealedcrypto`) |
| `ui/templates/waiting.html` | waiting page on the second server port (`{{CONTAINER_NAME}}`, `{{REDIRECT_URL}}`) |
| `ui/assets/**` | `/ui/assets/**` (shared runtime assets, theme backgrounds, PWA manifests) |
| `ui/assets/vendor/**` | `/ui/assets/vendor/**` (vendored Alpine, htmx and Tailwind builds) |
| `ui/service-worker.js` | `/ui/service-worker.js` (one hour cache, `Service-Worker-Allowed: /`) |
| `ui/sw-register.js` | `/ui/sw-register.js` (one hour cache) |
| `ui/assets/vite.ico` | `/favicon.ico` (one day cache) |
| - | `/` redirects to `/ui` |

- The Go server appends a content hash (`?v=...`) to the `app.js`, `home.js`, `ghostboard.js` and
  vendored library tags: keep the tags pointing at the served paths. The list of rewritten
  references lives in `internal/api/route/ui_route.go`, so a new `<script>` or `<link>` needs an
  entry there or it will be cached for a year without a version.
- `ui/ghostboard.html` has a **Preview** toggle: `ghostboardMarkdown()` in `ui/assets/ghostboard.js`
  renders headings, lists, fenced code, blockquotes, rules, tables with column alignment, emphasis,
  inline code, `[text](url)` links and bare `http(s)` URLs and email addresses. The preview is built
  from escaped text and rendered with `x-html`, so it must never gain an HTML passthrough: the
  renderer escapes first and only then adds its own tags. Generated tags are stashed as placeholders
  while the inline passes run, so a URL in a link label is not autolinked into a nested anchor and
  emphasis is not applied inside code. Table rows split on pipes that are neither escaped (`\\|`) nor
  inside a code span. Renders are debounced and skipped above `GHOSTBOARD_PREVIEW_MAX_CHARS`
  (262 144) so a multi-megabyte board cannot freeze the tab; the preview is refreshed from
  `applyPayload()`, so a remote change updates it too.
- `ui/ghostboard.html` renders the board and, for a board listed in `ghostboard.private_boards`,
  a PIN form in its place. The page loads the board state before opening the `EventSource`, so a
  locked board never leaves a stream retrying against a `401`; when a save answers `401` the form
  comes back and the locally typed text is kept until the unlock writes it. The server ends the
  stream with a `locked` event when the session expires: the page listens for it and shows the
  same form through `sessionExpired()`, so it must stay a single handler. A `429` on the unlock
  carries `retry_after_secs` and the page shows how long the lockout lasts.
- `ui/ghostboard.html` carries the **📎 Files** panel above the textarea. It is available in edit
  and preview mode, hidden while the page is locked, and driven by `data-max-file-bytes` that
  `serveGhostboardPage` injects (an empty value means the server disabled attachments and the panel
  stays hidden). Uploads use `XMLHttpRequest`, not `fetch`: only XHR reports `upload.onprogress`
  and can abort a transfer, which is what the per-file percentage and the cancel button need. One
  file per request, sent one after the other; the server answers `201` with the stored metadata and
  the page prepends it to `files`. Names are rendered with `x-text` and `:download`, never with
  `x-html`; the delete button asks `window.confirm` per file, and **Download all (.zip)** links the
  one archive route so the whole board moves in a single request. The saved light/dark choice is
  applied before paint by `ui/assets/ghostboard-theme.js`, a synchronous script in the head:
  keeping it out of an inline `<script>` is what lets the server's Content-Security-Policy refuse
  inline scripts. The Clear button opens an inline
  dialog rather than `window.confirm`, because it offers two choices: clear the text only, or clear
  the text and the attachments (`DELETE .../files` followed by the empty text).
- `applyPayload()` applies `payload.files`, so a peer's upload or delete appears in an open panel
  through the same event stream as the text. `showLock()` and `sessionExpired()` call `resetFiles()`:
  a locked page must not keep the list, and the transfers in flight are aborted.
- `ui/sealed.html` and `ui/assets/sealed.js` are the only page that handles a decryption key. Rules
  that follow from that: the page carries its own `Content-Security-Policy`
  (`middleware.SealedPageContentSecurityPolicy`) with no CDN origin and no `worker-src`, it is served
  with `Cache-Control: no-store`, it does **not** include `sw-register.js`, and it never writes the
  key to `localStorage`, `sessionStorage` or a cookie: the key lives in `location.hash` only. A
  third-party script there could read both the key and the plaintext, which is why the policy is
  narrower than the dashboard one and why the page loads only the vendored Alpine and its own
  script. `window.isSecureContext` decides whether decryption is possible at all: outside HTTPS (or
  `localhost`) the page refuses and prints the equivalent `gb-cli` command. The reveal of a burn
  board is always a click, never an automatic fetch, and every request uses `cache: "no-store"`.
  `window.sealedSelfTest()` seals the shared vector with its published IV and compares the result,
  so the WebCrypto code and `internal/sealedcrypto` cannot drift apart; run it from the browser
  console or from the end-to-end check. Creation is reached from the other pages: `ui/ghostboard.html`
  and `ui/home.html` carry a `<!-- SEALED_BOARD_ENTRY -->` marker that the route replaces with a
  navigation-only link to `/ui/sealed` when `ghostboard.sealed_enabled` is on; the key is never
  handled by a page whose policy allows remote origins.
- `ui/assets/app.js` and `ui/home.js` are served as-is (single-line, minified). Edit them directly,
  keep dead code out, and do not rename them without updating the cache-buster paths in
  `internal/api/route/ui_route.go`.

### Vendored libraries

| File | Library | Source |
| ---- | ------- | ------ |
| `ui/assets/vendor/alpine-3.17.5.min.js` | Alpine.js 3.17.5 | `npm alpinejs@3.17.5`, `dist/cdn.min.js` |
| `ui/assets/vendor/alpine-collapse-3.17.5.min.js` | Alpine collapse plugin 3.17.5 | `npm @alpinejs/collapse@3.17.5`, `dist/cdn.min.js` |
| `ui/assets/vendor/htmx-1.9.10.min.js` | htmx 1.9.10 | `npm htmx.org@1.9.10`, `dist/htmx.min.js` |
| `ui/assets/vendor/tailwind-2.2.19.min.css` | Tailwind 2.2.19 (full build) | `npm tailwindcss@2.2.19`, `dist/tailwind.min.css` |

Tailwind is the full pre-built stylesheet (~2.9 MB), which is the price of no toolchain and no
CDN; the browser and the service worker cache it once. Upgrading a library means: download the new
file with its version in the name, update the tags in `index.html`, `home.html` and
`ghostboard.html`, update the constants in `internal/api/route/ui_route.go`, add the file to the
precache list in `ui/service-worker.js` and bump its `CACHE_NAME`. The Alpine collapse plugin and
Alpine core must stay on the same version, and the plugin tag must stay before the core one.
`ui/assets/vendor/NOTICES.md` records versions and licenses: keep it in step with the files.
`TestUIRoute_PagesLoadNoExternalAssets` fails if a page goes back to a CDN, and
`TestUIRoute_ServiceWorkerPrecacheFilesExist` fails if the install list and the files disagree.

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

0. The sealed page is deliberately outside the worker: it is not in the precache list and does not
   register it, so a burned envelope can never be replayed from a cache.
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
  fixtures it serves live in `scripts/screenshots/fixtures/`. The UI draws its icons with emoji, so
  a host without an emoji font captures empty boxes: `task screenshots` warns, and either install
  `fonts-noto-color-emoji` or extract the package into `.build/` and point `FONTCONFIG_FILE` at a
  config that includes the system one and adds that directory:

  ```bash
  dpkg-deb -x fonts-noto-color-emoji_2.051-1_all.deb .build/screenshot-fonts/fonts
  FONTCONFIG_FILE="$PWD/.build/screenshot-fonts/fonts.conf" task screenshots
  ```
- Gates before a PR: `go vet ./...`, `golangci-lint run --concurrency=1 ./...`, `go test ./...`
  (at minimum `go test ./internal/api/route` for UI-only changes).
