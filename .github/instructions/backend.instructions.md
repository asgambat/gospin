---
applyTo: "**/*.go"
---
<!-- The above section is called 'frontmatter' and is used to define metadata for the document -->
<!-- The main content of the markdown file starts here -->
# Backend Development Guidelines

GoSpin's backend is pure Go (1.25.6, module `github.com/bassista/go_spin`): a gin HTTP API, a
Docker runtime abstraction, a scheduler and a JSON-backed repository. These rules specialize
the repository-wide guide in `AGENTS.md`, which remains the authoritative source.

## General Guidelines

Follow idiomatic Go and the conventions already established in the codebase. Prefer the
standard library; add a dependency only when the project already relies on that class of
library (gin, viper, logrus, Docker SDK).

- **Package layout**: keep the entrypoint in `cmd/server/`, everything else in `internal/`
  (`api/route` for route registration, `api/controller` for handlers, `api/middleware` for
  cross-cutting HTTP concerns, `app` for wiring, `cache`, `config`, `cosmos`, `logger`,
  `repository`, `runtime`, `scheduler`, `version`).
- **Consistency**: extend the existing pattern instead of inventing a parallel one.
- **Error Handling**: return errors with context, never swallow them, log once at the layer
  that owns recovery.
- **Configuration Management**: every tunable value comes from `internal/config`; no magic
  numbers, ports or timeouts in handlers.
- **Testing**: table-driven tests in the same package, `net/http/httptest` for routes.

## Go Conventions

- Format with `gofmt` (tabs); exported identifiers carry a `//` doc comment in English.
- Accept interfaces where they are consumed, return concrete types; keep interfaces small.
- Propagate `context.Context` through HTTP handlers, repository calls, the Docker runtime and
  the scheduler; honour cancellation and deadlines.
- Prefer injectable collaborators over package-level mutable state. Wiring lives in
  `internal/app`.
- Guard shared state (cache, scheduler bookkeeping) with `sync.Mutex`/channels; run
  `task test:race` (`go test -race ./...`) when touching concurrency. On a Raspberry Pi 4 the
  race detector cannot be built: skip `-race` there and report it.

<a name="backend-architecture"></a>

## Architecture & Structure

Keep the existing layered flow:

- `internal/api/route` registers routes on the gin engine.
- `internal/api/controller` implements handlers: bind/validate input, call the next layer,
  shape the HTTP response.
- `internal/repository`, `internal/cache`, `internal/runtime`, `internal/scheduler`,
  `internal/cosmos` hold the business and integration logic.
- `internal/app` assembles the dependencies; handlers do not construct their own clients.

Rules:

- Keep HTTP DTOs separate from domain/state types; map at the edge.
- Route handlers stay thin: no Docker SDK calls, no direct file writes.
- A new endpoint means: route in `internal/api/route`, handler in `internal/api/controller`,
  Go test in the route package, and a row in the README API Endpoints table.

<a name="backend-error-handling"></a>

## Error Handling

- Wrap errors with context: `fmt.Errorf("loading actions config: %w", err)`; use `errors.Is`/
  `errors.As` when a caller must branch on a cause.
- Fail fast on invalid configuration at startup (`internal/config` validates and returns an
  error).
- Map known failures to stable HTTP status codes and a JSON body with an `error` field, as the
  existing controllers do.
- Log once, at the layer that handles the failure; do not log-and-return at every level.
- Tests must cover the error and edge-case paths (see
  `.github/copilot-instructions.md#quality-policy`).

<a name="backend-observability"></a>

## Observability

- Log through `internal/logger`: `logger.WithComponent("route")` returns a logrus entry with
  the `component` field. Never add `fmt.Println`, `log.Printf` or a second logger.
- Levels: **Debug** for internals, **Info** for operational events, **Warn** for recoverable
  issues, **Error** for failures that need attention.
- One message per event: avoid duplicated log lines describing the same condition.
- Never log secrets (`GO_SPIN_COSMOS_TOKEN`), full configuration dumps or request bodies
  containing credentials.

<a name="backend-security"></a>

## Security Essentials

- Input validation at the boundary: validate names, URLs and schedule payloads before they
  reach the runtime or the repository.
- CORS and timeouts come from configuration (`misc.cors_allowed_origins`, `server.request_timeout_millis`
  and the timeout keys), not from literals in code.
- The process mounts `/var/run/docker.sock`: treat container operations as privileged, keep
  error responses free of internal paths and tokens.
- Secrets load from environment variables or the config file, never from source; do not commit
  `config/config.yaml` values that contain credentials.

## API Development

1. **Endpoint design**: consistent, RESTful paths grouped by feature (containers, groups,
   schedules, runtime, configuration, homepage, UI).
2. **Request/response formats**: JSON, camelCase where the existing endpoints already use it.
3. **Handlers**: bind with explicit structs, return early on bind/validation errors, and keep
   the response construction in one place.
4. **Documentation**: a new or changed endpoint updates the README API Endpoints table and its
   request/response/error description.

## Quality Gates

Run before finishing any change:

```bash
go fmt ./...
go vet ./...
staticcheck ./...                                # make lint includes it
golangci-lint run --concurrency=1 ./...
go test ./...
task test:cover                                  # per-package coverage check
```

`make lint` and `task check` bundle the same gates; `task ci` adds `test:race` and a fast
build. Coverage floor: **70% per package** — add tests rather than lowering the bar. CI
(`.github/workflows/tests.yml`) runs vet, staticcheck, golangci-lint and `go test -race ./...`.

References:

- Agent guide and authoritative policies: `AGENTS.md`
- Coverage and critical-path rules: `.github/copilot-instructions.md#quality-policy`
- Architecture details: `docs/ARCHITECTURE.md`, `docs/progetto.txt` (Italian)

<!-- © Capgemini 2025 -->
