<!--
SECTION PURPOSE: Frontmatter defines scope (which files are governed by these rules).
PROMPTING: Keep the YAML minimal; AI should respect this glob when proposing edits.
-->
---
applyTo: "ui/**/*.js, ui/**/*.html, ui/**/*.json, ui/**/*.css"
---

<!--
SECTION PURPOSE: Introduce mandatory frontend guidance.
PROMPTING: Clear headings; concise bullets for scanability.
COMPLIANCE: Treat rules below as defaults unless project overrides exist.
-->
# Frontend Instructions

The whole UI lives in `ui/` and is plain, hand-written HTML and JavaScript served directly by
the Go binary (`internal/api/route/ui_route.go`). There is **no** Node toolchain: no
`package.json`, no bundler, no transpiler, no `node_modules`.

<CRITICAL_REQUIREMENT type="MANDATORY">

- Write plain JavaScript (ES2020+) and HTML only. ❌ No TypeScript, JSX/TSX, React, Vue, Svelte, or any compile step.
- Alpine.js components are plain object factories (`function app() { return { ... } }`) defined in `ui/assets/app.js` (dashboard) and `ui/home.js` (homepage).
- Bump `CACHE_NAME` in `ui/service-worker.js` whenever you add, remove or rename a precached asset.
- Enforce accessibility: semantic HTML first; ARIA complements, not substitutes.
- Test the UI through the Go tests in `internal/api/route/ui_route_test.go`; there is no JavaScript test runner.

</CRITICAL_REQUIREMENT>

<!--
SECTION PURPOSE: Universal rules for all frontend code.
PROMPTING: Imperative checklist for quick verification.
-->
## General Guidelines

1. **Code Structure**: Keep the Alpine component factories as single `function ...() { return { state, methods } }` objects; group related helpers as methods, not globals. Shared runtime assets go in `ui/assets/`.
2. **Styling**: Use Tailwind utility classes (loaded from the CDN pinned in the HTML) and the existing `theme-<name>` body classes. ❌ Do not introduce a local CSS build (the Makefile's `tailwind-install`/`css` targets are legacy) and avoid inline styles except values driven by configuration (e.g. font size from `settings`).
3. **Accessibility**: Prefer native controls, clear labels, visible focus, sufficient contrast, keyboard operability (the homepage already supports `/`-style search and `Escape`).
4. **Dependencies**: Only the CDN scripts already pinned in the HTML may be used (Alpine.js 3, htmx 1.9.10, Tailwind 2.2.19). Adding any other library requires maintainer approval — remember it also ships to offline PWA users.
5. **Language**: Comments and identifiers in English; user-visible strings follow the existing copy of the page you edit.

<!--
SECTION PURPOSE: Expectations when authoring components.
PROMPTING: Specify contract (props/state), error modes, and data flow norms.
-->
## Alpine.js Data Components

1. **State**: Keep all state inside the returned object (`data`, `containers`, `theme`, ...); call `init()` for wiring, timers and first fetch. ❌ Do not use module-level mutable variables.
2. **API Calls**: `fetch` against the Go endpoints, with `this.apiBase` for the dashboard routes and absolute paths (`/homepage`, `/homepage/action`, `/runtime/system-stats`) for the homepage. Always check `response.ok`, surface failures through the existing `showError`/`showSuccess` helpers, and `encodeURIComponent` every path segment.
3. **Configuration over magic numbers**: polling intervals and intervals shown in the UI must come from the server (`settings.pollingIntervalSeconds`, `settings.statsPollingIntervalSeconds`, `/configuration`), never hardcoded.
4. **Persistence**: User preferences use the existing localStorage keys (`homepage-theme`, `homepage-collapsed-groups`); wrap `JSON.parse` in `try/catch` and reset to a safe default on failure.
5. **Cleanup**: Clear timers and listeners before re-registering them (`clearInterval` before `setInterval`), and guard against re-entrant actions with the existing `isXxx` flags.
6. **Escaping**: Render server data with `x-text`; ❌ do not use `x-html` with data coming from the API (XSS).
7. **Editing the files**: `ui/assets/app.js` and `ui/home.js` are served as-is (single-line, minified). Edit them directly, keep dead code out, and do not rename them without updating the cache-buster paths in `internal/api/route/ui_route.go`.

<!--
SECTION PURPOSE: Scope the HTML/PWA assets that ship with the binary.
PROMPTING: Concrete file-to-route mapping so edits land in the right place.
-->
## HTML, PWA and Service Worker

1. **Files and routes**: `ui/index.html` → `/ui` (dashboard), `ui/home.html` → `/ui/home` (homepage dashboard), `ui/templates/waiting.html` → waiting page served on port 8085, `ui/assets/**` → `/ui/assets/**`.
2. **Cache busting**: Go appends a content hash (`?v=...`) to the `app.js` / `home.js` script tags; keep the script tags pointing at the served paths.
3. **Service worker**: `ui/service-worker.js` precaches the app shell, both pages, theme backgrounds and icons. Bump `CACHE_NAME` (`gosspin-ui-vN`) on every precache change and keep the list in sync with the files that actually exist.
4. **Caching contract**: assets are served with `Cache-Control: public, max-age=31536000`, except PWA manifests (`no-cache`). Do not weaken these headers without updating the service worker strategy.
5. **PWA metadata**: keep `ui/assets/pwa/*.json` manifests, icons and the `/ui` / `/ui/home` scopes consistent when adding pages or themes.

<!--
SECTION PURPOSE: Make testing guidance explicit and link to SSOTs (Tester agent and coverage policy).
PROMPTING: Reference, don't duplicate. Keep actions concrete for frontend work.
-->
## Testing

1. **SSOT References**
    - Tester agent: `.github/agents/Tester.agent.md`
    - Coverage policy: `.github/copilot-instructions.md#quality-policy`

2. **Go-level verification (default)**
    - `ui/*.js` and `ui/*.html` have no unit tests; they are verified through
      `internal/api/route/ui_route_test.go` with `httptest`.
    - When you change what a UI route serves (status code, content type, cache headers, injected
      cache-buster, fallback behaviour), extend those Go tests.

3. **Manual verification**
    - `task run` (or `make run`), then open `/ui` and `/ui/home`; check the browser console is
      free of errors, polling updates the page, and a hard reload after a service-worker bump
      still works.
    - Exercise the action modal / container controls you touched; report anything you could not
      verify instead of assuming it works.

4. **Coverage Policy**
    - Follow the central Quality & Coverage Policy in `.github/copilot-instructions.md#quality-policy`
      (70% floor per package, enforced on the Go packages).

<!--
SECTION PURPOSE: Keep apps fast and responsive.
PROMPTING: Short, actionable techniques.
-->
## Performance Optimization

1. **Keep the payload small**: the HTML, `app.js` and `home.js` are shipped on every first load; prefer extending the existing Alpine objects over adding libraries.
2. **Avoid DOM churn**: update state instead of recreating large sections; keep `x-for` keys stable.
3. **Polling cost**: reuse the configured intervals, stop timers on hidden pages where the code already does so, and avoid stacking intervals.
4. **Images**: reference the bundled theme assets (`.webp` preferred) through absolute `/ui/assets/...` paths so both the router and the service worker can cache them.
5. **No memoization framework**: there is no React-style render loop — debounce expensive computations in plain JavaScript if needed.

<!--
SECTION PURPOSE: Enforce baseline quality gates.
PROMPTING: XML block for machine-checkable rules.
-->

<PROCESS_REQUIREMENTS type="MANDATORY">

- Run the Go gates before opening a PR: `go vet ./...`, `golangci-lint run --concurrency=1 ./...`, `go test ./...` (at minimum `go test ./internal/api/route` for UI-only changes).
- Bump the service-worker `CACHE_NAME` in the same change that modifies the precache list.
- No TypeScript, no build artifacts, no vendored JavaScript libraries: the files under `ui/` are the source.
- Remove debugging `console.log` noise; keep `console.warn`/`console.error` for real failures.
- Include accessibility checks (labels, keyboard nav, focus order) in reviews.

</PROCESS_REQUIREMENTS>

<!-- © Capgemini 2025 -->
