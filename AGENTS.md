# AGENTS.md — GoSpin Agent Guide

GoSpin is a Go 1.25.6 service (module `github.com/asgambat/gospin`) that manages Docker
containers and serves a dashboard plus a JSON API.

This file is the **single source of truth** for how agents work in this repository. It carries
the rules an agent must always have in context and links to the documents holding the long form
of each topic (see [Documentation](#documentation)); when this file and a linked document
disagree, this file wins.

## Project Shape

| Path | Purpose |
| ---- | ------- |
| `cmd/server/` | Application entrypoint (`main.go`) |
| `cmd/board/` | `gb-cli` CLI entrypoint: reads and writes ghostboards from the shell |
| `internal/api/route/` | HTTP route definitions; one file per feature area |
| `internal/api/controller/` | HTTP handlers and business logic |
| `internal/api/middleware/` | CORS, timeout, request logging middleware |
| `internal/app/` | Application container / dependency wiring |
| `internal/config/` | Configuration loading (viper + `GO_SPIN_` env prefix) |
| `internal/logger/` | Logger (logrus) and `WithComponent` helper |
| `internal/cache/`, `internal/repository/` | In-memory state and persistence |
| `internal/runtime/` | Container runtime abstraction (Docker / memory) |
| `internal/scheduler/` | Spin-up/spin-down scheduling loops (goroutines) |
| `internal/ghostboard/` | Shared text boards behind `/ui/ghostboard` (store, JSON persistence, SSE) |
| `internal/boardclient/` | HTTP client and configuration resolution of the `gb-cli` CLI |
| `internal/cosmos/` | Cosmos API client |
| `internal/version/` | Build metadata injected via `-ldflags` |
| `config/` | Runtime YAML (`config.yaml`, `homepage.yaml`, `actions.yml`) and `config/data/` |
| `ui/` | Static HTML/JS embedded and served by the binary |
| `scripts/screenshots/` | Regenerates `docs/screenshots/` from a demo instance (fixtures + Playwright driver) |
| `docs/` | Architecture, development notes, examples, ADRs, PRDs, plans |
| `.github/` | Workflows, branch/PR conventions and the per-area agent instruction supplements |
| `Taskfile.yml` | The only build front-end (see Build Commands) |

The UI is plain HTML + JavaScript (Alpine.js, htmx and Tailwind loaded from CDNs). There is
**no `package.json`, no Node toolchain, no TypeScript and no JS build step** in this repo; the UI
rules (files → routes, Alpine patterns, PWA and service worker, UI testing) are in
[docs/FRONTEND.md](docs/FRONTEND.md).

Node is used by exactly one thing: `task screenshots`, the documentation screenshot capture. It
needs `node`/`npm` on the machine, installs Playwright into `.build/screenshot-deps` (gitignored)
and never touches the application build or the shipped assets.

## Build Commands

`Taskfile.yml` is the only build front-end; there is no Makefile. The legacy Tailwind
`tailwind-install`/`css` targets went away with it — the UI loads Tailwind from a CDN, so
nothing compiles CSS locally and `task install` only installs air.

```bash
# Toolchain prerequisites
go version            # 1.25.6+ (see go.mod)
task install          # install dev tooling: air, used by `task watch`

# Build (Taskfile → .build/gospin, runs lint + test first)
task build
task build:fast       # build without lint/test
task build:cli        # build the gb-cli CLI into .build/gb-cli

# Run / hot reload
task run              # go run with version metadata injected
task watch            # air, configured by .air.toml

# Documentation
task screenshots      # regenerate docs/screenshots (needs node; Playwright goes into .build/)

# Quality gates
task lint             # staticcheck + golangci-lint
task check            # go fmt + go vet + staticcheck + golangci-lint + go test
task ci               # go mod download/verify + lint + test:race + build:fast
```

`task screenshots` runs `scripts/screenshots/capture.sh`: it builds the server, starts it on the
memory runtime with the fictional data in `scripts/screenshots/fixtures/`, drives Playwright's
Chromium over `/ui`, `/ui/home` and `/ui/ghostboard/demo`, and writes the five PNGs to
`docs/screenshots/`. Never point it at a real instance — the fixtures are what keep hostnames out of
the documentation. A host without an emoji font renders the UI icons as boxes; the script warns.
Add or change UI content that appears in a screenshot in the same change that regenerates it.

## Linting Rules

- `golangci-lint run --concurrency=1 ./...` with `.golangci.yml` (staticcheck, govet,
  errcheck, bodyclose, ineffassign, unused and the rest of the enabled set).
- `go vet ./...`, `go fmt ./...` and `staticcheck ./...` are part of the gate as well.
- CI (`.github/workflows/tests.yml`) pins `golangci-lint v1.64.8` and `staticcheck 2025.1.1`.
- Fix the cause of a finding. Do not disable linters, add `nolint` directives or weaken
  checks to make a build go green; if a suppression is genuinely required, justify it inline.

## Testing Procedures

Tests use the standard library: `testing`, table-driven cases, `net/http/httptest` for routes.
Keep test files next to the code they cover (`*_test.go`, same package).

```bash
go test ./...                                              # all tests
task test                                                  # same, -count=1
task test:verbose                                          # -v
task test:race                                             # race detector (see caveat below)
task test:cover                                            # coverage.out + coverage.html
task test:package -- ./internal/config                     # single package
go test ./internal/api/route -run TestUI -v                # single test
```

- **Always run the full test suite after a change** to catch regressions.
- New functionality needs unit tests; new or changed endpoints and scheduling/runtime
  behaviour need tests that exercise the HTTP/state path.
- **Coverage target: 70% per package — recommended, never a CI gate.** No workflow blocks a
  build on coverage: `.github/workflows/coverage.yml` only measures and publishes the report
  and badge, and `.github/workflows/tests.yml` runs vet/staticcheck/golangci-lint/`-race`
  tests without a coverage threshold. A number below the target is therefore a quality signal
  for you and the reviewer, not a failure to fix by skipping tests.
  - New functionality — a new package, endpoint, scheduler/runtime behaviour or config key —
    means writing the tests that cover it, then re-measuring with `task test:cover` or
    `go test -cover ./...`.
  - If that measurement leaves a package below 70%, say so explicitly in your final summary
    (name the package and the percentage) and either add the missing tests or state why they
    are out of scope for this change. Never lower the target to make the number look better.
  - Hot paths (request handling, scheduler loops, persistence) and security-relevant logic
    (input validation, token handling) need 100% of their new branches covered.
  - A package that cannot reach the target for a documented reason needs a
    `Coverage Exception:` section in the PR description (scope, rationale, risk mitigation)
    plus a reviewer acknowledgment — the process is in
    [docs/engineering/code-review-guidelines.md](docs/engineering/code-review-guidelines.md).
- Report checks that could not run as limitations, never as passes.

### Concurrency Tests

Scheduler, cache and runtime code use goroutines and channels. When touching it, verify
synchronisation and run `task test:race` (`go test -race ./...`). Before running the race
detector, check whether the host is a Raspberry Pi 4: its toolchain cannot build with the
race flag, so skip `-race` there and say so explicitly in the summary.

## Iterative Improvement Process

Follow an incremental workflow: small changes that improve quality, readability and
maintainability, verified after each step.

1. Make the smallest coherent change.
2. Run `task lint` and `go test ./...`.
3. Fix what breaks before starting the next iteration.
4. Use the existing logging and error-handling conventions (below).

## Refactoring Guidelines

- Follow idiomatic Go: small interfaces at the consumer, explicit dependencies, no `init`
  side effects beyond what already exists.
- Keep refactorings minimal yet effective; core behaviour must not change.
- Avoid duplicate code and redundant log statements. Each log message must carry unique,
  actionable information about state or behaviour; if two messages say the same thing,
  collapse them.

## Configuration over Magic Numbers

Do not hardcode ports, timeouts, intervals, paths, limits or any other tunable value.
Configuration lives in `internal/config` (viper, defaults via `viper.SetDefault`, env
overrides prefixed `GO_SPIN_`) and in `config/config.yaml`.

```go
// ✅ new tunable value
viper.SetDefault("server.request_timeout_millis", 1000)
// ❌ magic number buried in a handler
time.Sleep(10 * time.Second)
```

## Logging and Error Handling Guidelines

- Use the existing logger: `logger.WithComponent("route")` from `internal/logger`
  (logrus-based). Never add `fmt.Println` or a competing logger.
- Levels: **Debug** for detailed internals, **Info** for operational events, **Warn** for
  potential issues, **Error** for failures requiring attention.
- Handle every error explicitly; never swallow one. Wrap with context:
  `fmt.Errorf("loading actions config: %w", err)`, and use `errors.Is`/`errors.As` when a caller
  must branch on a cause.
- Fail fast on invalid configuration at startup: `internal/config` validates it and returns an
  error instead of limping along with a partial config.
- Map known failures to stable HTTP status codes and a JSON body carrying an `error` field, the
  way the existing controllers do.
- Do not log the same failure at multiple layers: return the wrapped error up and log it once, at
  the layer that owns recovery.
- Never log secrets, tokens or full configuration dumps (see `GO_SPIN_MISC_COSMOS_TOKEN`).

## Architecture and Layering

- HTTP lives at the edge: `internal/api/route` registers the routes,
  `internal/api/controller` holds the handlers, `internal/api/middleware` holds the cross-cutting
  HTTP concerns.
- Handlers stay thin: bind and validate the input, call the next layer, shape the response. No
  Docker SDK call and no direct file write inside a handler.
- Keep the HTTP request/response types separate from the domain and state types, and map between
  them at the edge.
- Business and integration logic lives in `internal/cache`, `internal/repository`,
  `internal/runtime`, `internal/scheduler` and `internal/cosmos`; dependencies are assembled only
  in `internal/app`, so a handler never builds its own client.
- Never add a top-level package outside `cmd/` and `internal/`.
- Prefer injectable collaborators over package-level mutable state.

## Security Essentials

- Validate at the boundary: names, URLs and schedule payloads are checked before they reach the
  runtime or the repository.
- CORS origins and every timeout come from configuration (`misc.cors_allowed_origins` and the
  timeout keys), never from literals in the code.
- The process mounts `/var/run/docker.sock`: treat container operations as privileged and keep
  error responses free of internal paths and tokens.
- Secrets arrive from the environment or the config file, never from source: do not commit
  credentials in `config/config.yaml` and never log them.

## Versioning and Releases

- `.version` holds the current app version and `internal/version/version.go` mirrors it as
  the compiled default; build flags set `git describe --tags` values when tags exist.
- `task version:bump -- 0.0.14` runs `.task/version_bump.sh`, which rewrites every
  version-bearing file (`.version`, `internal/version/version.go`, `Taskfile.yml`,
  `Dockerfile`, `README.md` and both docker workflows), commits
  `chore: bump to 0.0.14` and tags `v0.0.14`. Always go through the task — never edit the
  version by hand in only one file.
- The bump reads the current value from `.version` and refuses a version that is not
  `MAJOR.MINOR.PATCH`, is lower than or equal to the current one, already has a `v<version>`
  tag, or disagrees with the compiled default in `internal/version/version.go`. Published
  registry tags are never moved backwards, so a rejected bump must be fixed in the files
  first, never by re-tagging.
- Build metadata is injected into `internal/version` (`Version`, `BuildTime`, `GitCommit`,
  `GoVersion`) via `-ldflags "-X ..."`. The same flags live in three places and must stay in
  lockstep: `Taskfile.yml` (`VERSION_FLAGS`/`LDFLAGS`, used by `build`, `build:fast`,
  `build:cli` and `run`), `.air.toml` `[build].cmd` and the `Dockerfile` `ARG` + `-ldflags`
  block.
  Both GoSpin and `gb-cli` are built with these flags, so the two binaries always report the
  same version.
- Pushing the `vX.Y.Z` tag is what publishes the image (`docker-release.yml`, ghcr + Docker Hub).
  `latest` follows every `0.x` release and, from `1.0.0` on, only a minor/major tag
  (`X.Y.0`/`X.0.0`); every release publishes its `X.Y.Z`, `X.Y` and `X` tags, and pushes to `main`
  publish the `main` tag without touching `latest`.
- Docker: `task docker:build`, `task docker:buildx` (multi-arch push),
  `task docker:run` (exposes 8084/8085 and mounts `/var/run/docker.sock`).

## Coding Conventions

- Idiomatic Go, `gofmt` formatting, tabs as produced by `go fmt`. No TypeScript, Prettier or
  ESLint conventions apply in this repository.
- Package names short and lowercase; exported identifiers carry a `//` doc comment that starts
  with the identifier name.
- Extract helpers in the same package instead of adding a new `util` package, and keep functions
  small and single-purpose.
- Prefer the standard library; add a dependency only when it clearly pays for itself and is
  already the established choice (gin, viper, logrus, Docker SDK, ...).
- Propagate `context.Context` through request and scheduler paths; respect cancellation. Close
  channels on the owner side and avoid goroutine leaks.
- Comments, log messages and documentation in English. The legacy plan
  `docs/plans/archive/HTMX_MIGRATION_PLAN.md` is the only Italian file left in the repository: do
  not add new Italian docs, and write its follow-ups in English.

## Documentation

Keep documentation in sync with the code after every relevant change:

- **README.md** — user-facing behaviour: API endpoints table, configuration options (YAML
  example plus environment variables), usage patterns, screenshots.
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — component layout, data flow and the design
  patterns behind them.
- [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) — local setup, build, test, release and Docker
  workflows.
- [docs/EXAMPLES.md](docs/EXAMPLES.md) — configuration, monitoring and troubleshooting recipes.
- [docs/FRONTEND.md](docs/FRONTEND.md) — UI rules: files and routes, Alpine component patterns,
  PWA/service worker, accessibility, UI testing.
- [docs/CLI.md](docs/CLI.md) — `gb-cli` reference: commands, output modes, exit codes and
  configuration resolution.
- [docs/README.md](docs/README.md) — the index of this documentation set, for humans browsing
  `docs/`.
- **CHANGELOG.md** — user-visible changes; new entries go under `## [Unreleased]` and the release
  commit moves them under the new version heading.
- [docs/engineering/code-review-guidelines.md](docs/engineering/code-review-guidelines.md) —
  review checklist, severity taxonomy and the coverage-exception process.
- **docs/ADRs/**, **docs/PRDs/**, **docs/plans/** — architectural decisions, requirements and
  active plans.
- **.github/agents/*.md** — the agent roles; **.github/prompts/*.md** — reusable prompt
  templates; **.github/branching-strategy.md** — branch, commit and PR conventions.
- **AGENTS.md** (this file) — agent rules; update it when process, test or documentation
  conventions change.

When adding an endpoint:

- Add a row to the README API Endpoints table with method, path and a short description.
- Describe request/response shape and error codes.
- Document any new configuration key, its default and its environment override.
- Add the route in `internal/api/route/`, the handler in `internal/api/controller/` and tests.

When adding a configuration option:

- Define it in `internal/config` with a sensible default and validation.
- Add it to `config/config.yaml` and to the README YAML example.
- Document the `GO_SPIN_*` environment override (if any).
- Update `docs/ARCHITECTURE.md` (and the matching README prose) if it affects scheduling,
  runtime or persistence.
- Add a test in `internal/config` for the new option and its default.

When writing documentation (any `.md` file, ADRs, PRDs, plans):

- Organize it into clear sections with headings, short sentences and fenced code blocks carrying a
  language identifier; link to the related docs instead of restating them, and skip emojis and
  informal language.
- Save it in its canonical location and use the existing templates: ADRs in `docs/ADRs/`
  (`adr-template.md`), PRDs in `docs/PRDs/` (`prd-template.md`), plans and design docs in
  `docs/plans/` (`plan-template.md`), engineering guidelines in `docs/engineering/`.
- Keep links valid, and update `docs/README.md` and any README table of contents when you add or
  remove a document.

## Language Consistency

All new comments, log messages and documentation are written in English; the legacy
`docs/plans/archive/HTMX_MIGRATION_PLAN.md` is the only Italian file still in the repository.

## Agent Instruction Files

- `AGENTS.md` (this file) is authoritative, and it is the file GitHub Copilot and the other
  assistants read first. When a rule is needed by every agent, put it here.
- `.github/agents/*.md` (agent personas) and `.github/prompts/*.md` (prompt templates) are helpers
  for those assistants; they must not contradict this file. When they repeat a rule, change the
  rule here first and then the repetition.

## Maintenance of This File

- Update `AGENTS.md` when build commands, code style, testing procedures or documentation
  policy change, and update the linked documents in the same change. Applying that update is
  part of the change, not a separate proposal to wait on.
