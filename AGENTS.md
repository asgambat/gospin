# AGENTS.md — GoSpin Agent Guide

GoSpin is a Go 1.25.6 service (module `github.com/bassista/go_spin`) that manages Docker
containers and serves a dashboard plus a JSON API.

This file is the **single source of truth** for how agents work in this repository.
`.github/copilot-instructions.md` and `.github/instructions/*.md` mirror these rules for
GitHub Copilot; when they disagree with this file, this file wins.

## Project Shape

| Path | Purpose |
| ---- | ------- |
| `cmd/server/` | Application entrypoint (`main.go`) |
| `internal/api/route/` | HTTP route definitions; one file per feature area |
| `internal/api/controller/` | HTTP handlers and business logic |
| `internal/api/middleware/` | CORS, timeout, request logging middleware |
| `internal/app/` | Application container / dependency wiring |
| `internal/config/` | Configuration loading (viper + `GO_SPIN_` env prefix) |
| `internal/logger/` | Logger (logrus) and `WithComponent` helper |
| `internal/cache/`, `internal/repository/` | In-memory state and persistence |
| `internal/runtime/` | Container runtime abstraction (Docker / memory) |
| `internal/scheduler/` | Spin-up/spin-down scheduling loops (goroutines) |
| `internal/cosmos/` | Cosmos API client |
| `internal/version/` | Build metadata injected via `-ldflags` |
| `config/` | Runtime YAML (`config.yaml`, `homepage.yaml`, `actions.yml`) and `config/data/` |
| `ui/` | Static HTML/JS embedded and served by the binary |
| `docs/` | Architecture, development notes, ADRs, PRDs, `progetto.txt` |

The UI is plain HTML + JavaScript (Alpine.js, htmx and Tailwind loaded from CDNs). There is
**no `package.json`, no Node toolchain, no TypeScript and no JS build step** in this repo.

## Build Commands

Both `Makefile` and `Taskfile.yml` are supported and must keep working. `Taskfile.yml` is the
more complete front-end (it also covers Docker and version bumping). The Makefile's
`tailwind-install` and `css` targets are legacy from when Tailwind was compiled locally; the
UI now loads Tailwind from a CDN, so they are not part of the normal workflow.

```bash
# Toolchain prerequisites
go version            # 1.25.6+ (see go.mod)
make install          # install dev tooling: air + local Tailwind CLI

# Build (Makefile → ./.build/main, runs lint + test first)
make build

# Build (Taskfile → .build/gospin, runs lint + test first)
task build
task build:fast       # build without lint/test

# Run / hot reload
make run
task run              # go run with version metadata injected
make watch            # air, configured by .air.toml
task watch

# Quality gates
make lint             # go fmt + go vet + staticcheck + golangci-lint
task check            # go fmt + go vet + golangci-lint + go test
task ci               # go mod download/verify + lint + test:race + build:fast
```

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
- **Coverage: every package must stay at or above 70%.** If a change drops a package below
  that threshold, add the tests needed to bring it back before finishing. CI publishes the
  coverage report and badge (`.github/workflows/coverage.yml`) but does not gate on it, so
  the threshold is enforced by review and by you.
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
2. Run `make lint` (or `task lint`) and `go test ./...`.
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
viper.SetDefault("server.request_timeout_secs", 10)
// ❌ magic number buried in a handler
time.Sleep(10 * time.Second)
```

## Logging and Error Handling Guidelines

- Use the existing logger: `logger.WithComponent("route")` from `internal/logger`
  (logrus-based). Never add `fmt.Println` or a competing logger.
- Levels: **Debug** for detailed internals, **Info** for operational events, **Warn** for
  potential issues, **Error** for failures requiring attention.
- Handle every error explicitly; never swallow one. Wrap with context:
  `fmt.Errorf("loading actions config: %w", err)`.
- Do not log the same failure at multiple layers. Return wrapped errors up and log once, at
  the layer that owns recovery.
- Never log secrets, tokens or full configuration dumps (see `GO_SPIN_COSMOS_TOKEN`).

## Versioning and Releases

- `.version` holds the current app version and `internal/version/version.go` mirrors it as
  the compiled default; build flags set `git describe --tags` values when tags exist.
- `task version:bump -- 0.0.12` runs `.task/version_bump.sh`, which rewrites every
  version-bearing file (`.version`, `internal/version/version.go`, `Makefile`, `Taskfile.yml`,
  `Dockerfile`, `README.md`, `docs/progetto.txt` and both docker workflows), commits
  `chore: bump to 0.0.12` and tags `v0.0.12`. Always go through the task — never edit the
  version by hand in only one file.
- The bump reads the current value from `.version` and refuses a version that is not
  `MAJOR.MINOR.PATCH`, is lower than or equal to the current one, already has a `v<version>`
  tag, or disagrees with the compiled default in `internal/version/version.go`. Published
  registry tags are never moved backwards, so a rejected bump must be fixed in the files
  first, never by re-tagging.
- Build metadata is injected into `internal/version` (`Version`, `BuildTime`, `GitCommit`,
  `GoVersion`) via `-ldflags "-X ..."`. The same flags are duplicated in four places and must
  stay in lockstep: `Makefile` (`LDFLAGS`), `Taskfile.yml` (`build`, `build:fast`, `run`),
  `.air.toml` (`[build].cmd`) and `Dockerfile` (`ARG` + `-ldflags`).
- Docker: `task docker:build`, `task docker:buildx` (multi-arch push),
  `task docker:run` (exposes 8084/8085 and mounts `/var/run/docker.sock`).

## Coding Conventions

- Idiomatic Go, `gofmt` formatting, tabs as produced by `go fmt`. No TypeScript, Prettier or
  ESLint conventions apply in this repository.
- Package names short and lowercase; exported identifiers documented with a `//` comment.
- Prefer the standard library; add a dependency only when it clearly pays for itself and is
  already the established choice (gin, viper, logrus, Docker SDK, ...).
- Propagate `context.Context` through request and scheduler paths; respect cancellation.
- Comments and log messages in English. `docs/progetto.txt` is the only Italian document.

## Documentation

Keep documentation in sync with the code after every relevant change:

- **README.md** — user-facing behaviour: API endpoints table, configuration options (YAML
  example plus environment variables), usage patterns, screenshots.
- **docs/progetto.txt** — architecture and workflows (this file stays **in Italian**).
- **docs/ARCHITECTURE.md** — component layout and data flow in English.
- **docs/DEVELOPMENT.md** — local setup, build, test and Docker workflows.
- **docs/ADRs/** — architectural decisions, one record per decision.
- **AGENTS.md** and **.github/copilot-instructions.md** (plus `.github/instructions/*.md`) —
  agent rules; update them when process, test or documentation conventions change.

When adding an endpoint:

- Add a row to the README API Endpoints table with method, path and a short description.
- Describe request/response shape and error codes.
- Document any new configuration key, its default and its environment override.
- Add the route in `internal/api/route/`, the handler in `internal/api/controller/` and tests.

When adding a configuration option:

- Define it in `internal/config` with a sensible default and validation.
- Add it to `config/config.yaml` and to the README YAML example.
- Document the `GO_SPIN_*` environment override (if any).
- Update `docs/progetto.txt` if it affects scheduling, runtime or persistence.

## Language Consistency

All comments, log messages and documentation are written in English, with the single
exception of `docs/progetto.txt`, which remains in Italian.

## Agent Instruction Files

- `AGENTS.md` (this file) is authoritative.
- `.github/copilot-instructions.md` and `.github/instructions/*.md` are the Copilot-facing
  mirror; keep them consistent with this file.
- There is no `.cursor/rules/` directory in this repository. Do not create one or reference
  one as if it existed.

## Maintenance of This File

- Update `AGENTS.md` when build commands, code style, testing procedures or documentation
  policy change, and mirror the change in `.github/copilot-instructions.md`.
- Do not rewrite this file on your own initiative: propose the change and apply it when the
  maintainer asks for it.
