---
applyTo: "**/*.go"
---
# Go Instructions

`AGENTS.md` is the authoritative guide for this repository; this file only adds the Go
specifics. If the two disagree, follow `AGENTS.md` and flag the difference.

GoSpin targets **Go 1.25.6** (see `go.mod`), module `github.com/bassista/go_spin`.

## Layout

- `cmd/server/main.go` — entrypoint: load config, build the app container, start servers.
- `internal/` — all application code. Route registration in `internal/api/route`, handlers in
  `internal/api/controller`, middleware in `internal/api/middleware`, wiring in `internal/app`.
- Never add new top-level packages outside `cmd/` and `internal/`.

## Style

- Run `gofmt`; the repository uses tabs. `go vet ./...`, `staticcheck ./...` and
  `golangci-lint run --concurrency=1 ./...` must stay clean (that is what `make lint` runs).
- Comments and log messages in English (the only Italian document is `docs/progetto.txt`).
- Exported identifiers get a `//` doc comment that starts with the identifier name.
- Keep functions small and single-purpose; extract helpers in the same package instead of
  adding util packages.
- Prefer the standard library; the established dependencies are gin, viper, logrus, the
  Docker SDK and `godotenv`. Do not introduce a new library for something stdlib covers.

## Configuration

- All tunables come from `internal/config` (viper, defaults via `viper.SetDefault`, env
  overrides prefixed `GO_SPIN_`, YAML under `config/`). Never hardcode ports, timeouts,
  intervals, paths or limits.
- New options: add the `viper.SetDefault`, map it into `Config`, document it in
  `config/config.yaml`/`README.md`, and add a test in `internal/config`.

## Logging and Errors

- Use `internal/logger`: `logger.WithComponent("route")` for a component-scoped entry. No
  `fmt.Println`, `log.Printf` or second logger.
- Levels: Debug (internals), Info (operational), Warn (recoverable), Error (needs attention).
- Wrap errors with context and `%w`: `fmt.Errorf("starting container %s: %w", name, err)`.
  Check with `errors.Is`/`errors.As`; never discard an error silently.
- Log once, at the layer that handles the failure.

## HTTP and Concurrency

- Handlers stay thin (bind/validate → call layer → respond) and live in
  `internal/api/controller`; routes are registered in `internal/api/route` and tested there.
- Propagate `context.Context`; respect the configured timeouts.
- Scheduler, cache and runtime code is concurrent: protect shared state, close channels on the
  owner side, and avoid goroutine leaks (`go test -race ./...`; skip `-race` on a Raspberry
  Pi 4 and say so).

## Tests

- Table-driven tests in the same package as the code, named `*_test.go`.
- HTTP behaviour: `net/http/httptest` against the gin engine.
- Run `go test ./...` (or `task test`, `task test:race`, `task test:cover`,
  `task test:package -- ./internal/config`).
- Coverage floor: **70% per package**; add tests instead of lowering it.

## Before Finishing

```bash
gofmt -l .          # must print nothing
go vet ./...
staticcheck ./...
golangci-lint run --concurrency=1 ./...
go test ./...
```

`make lint`, `task check` and `task ci` bundle these gates. Update `README.md`,
`docs/progetto.txt` (Italian), `docs/ARCHITECTURE.md` and the agent docs when behaviour,
configuration or workflows change.
