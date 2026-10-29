# Changelog

All notable changes to GoSpin are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html). The file starts with the work
that has not been released yet; changes shipped in earlier releases are described by the git tags
and their commits.

## [Unreleased]

### Added

- `CHANGELOG.md` itself, so a release no longer has to reconstruct its own history.
- `docs/FRONTEND.md`: UI file/route map, Alpine.js conventions, PWA and service-worker rules.
- `docs/CLI.md`: full `gb-cli` reference, extracted from the README.
- Five screenshots in `docs/screenshots/` regenerated from a demo instance: containers, groups,
  schedules, homepage and ghostboard.
- Seven architecture decision records under `docs/ADRs/`: polling scheduler, ghostboard over SSE,
  homepage and actions proxy, build metadata and version bump, `gb-cli` as a separate binary, JSON
  persistence with dirty tracking, and the Taskfile as the only build front-end.
- `scripts/screenshots/` and `task screenshots`: one command regenerates `docs/screenshots/` from
  a demo instance (memory runtime, fictional fixtures) with Playwright's Chromium, which is
  installed under `.build/` on first use so the application build stays free of a Node toolchain.
- `docs/plans/DOCS_REMAINING_WORK.md`: the documentation items that still need a maintainer
  decision.
- Ghostboard PIN protection: the boards listed in `ghostboard.private_boards` need one shared PIN
  (4-64 characters) for reading and writing, the page shows a lock form, and the PIN is stored only
  as an HMAC keyed by `GO_SPIN_GHOSTBOARD_PIN_SECRET`. `POST /ghostboard/:board/unlock` returns a
  signed session token, failed attempts are throttled per address, and `gb-cli` gains `--pin`,
  `GO_SPIN_BOARD_PIN` and the `pin-hash` command (ADR-0008).

### Changed

- Build front-end is the Taskfile alone: `task build`, `task build:cli`, `task run`, `task watch`,
  `task lint`, `task check`, `task ci`. The Makefile and its Tailwind targets are gone.
- Go module renamed to `github.com/asgambat/gospin` (imports, `-ldflags` version injection,
  golangci-lint local prefixes); the published Docker image stays `bassista/gospin`.
- Documentation reorganized around `AGENTS.md` as the single source of truth: completed
  `docs/README.md` index, headings without emoji, `.github/branching-strategy.md` replaces
  `.github/branch-strategies-not-used.md`, and the obsolete notes (`docs/air.txt`,
  `docs/install_templ.txt`, `docs/docker_dev_hot_reload.txt`, `docs/progetto.txt`) are removed.
- The ADR, PRD and plan templates are trimmed to the sections this repository actually uses; the
  generic stakeholder, compliance, communication and rollback sections are gone.
- `README.md` aligned with the shipped product: project tree, feature list, homepage and Cosmos
  coverage, Taskfile-based development workflow, prerequisites including `task` and the linters.
- `README.md` and `docs/ARCHITECTURE.md` no longer repeat each other: the README keeps the project
  tree plus links, while the architecture guide owns the patterns, data flow, concurrency model,
  design trade-offs, measured performance and the resource footprint.
- The `/ui` route table left the README for `docs/FRONTEND.md`, which now also documents the
  favicon, the service worker, the `/` redirect and the rule for adding a theme.
- `docs/ARCHITECTURE.md` documents the homepage/actions proxy and the Cosmos import instead of
  leaving them out of the component deep dive.
- `config/config.yaml` and the README document the keys the code actually reads:
  `data.scheduling_enabled`, `data.scheduling_poll_interval_secs`, `server.request_timeout_millis`,
  `data.refresh_interval_secs`, `data.stats_refresh_interval_secs`,
  `data.system_monitor_mount_point` and `misc.scheduling_timezone`; the unused `misc.log_format`
  is gone.

### Removed

- `config/homepage.yaml_bk`, an unreferenced stale copy of `homepage.yaml`; `config/homepage.yaml`
  stays the example users copy and edit.

### Fixed

- Homepage: a `settings.theme` value the UI does not ship (for example `default`) left the page
  without its CSS variables, i.e. unstyled. The theme is now normalized to `auto` when the config is
  loaded, and the page also falls back to `auto` for a name it does not know.
- `docker-compose.yml` uses the canonical `GO_SPIN_MISC_CORS_ALLOWED_ORIGINS` key and the documented
  8084 main port; the Postman collection is `docs/gospin.postman_collection.json`.
- `.air.toml` and `.air_win.toml` inject the same build metadata as the Taskfile, so hot reload
  reports the git version instead of a hardcoded fallback.
- `AGENTS.md` examples pointed at a non-existent `server.request_timeout_secs` key and at the
  unprefixed `GO_SPIN_COSMOS_TOKEN`.

## [0.0.13] - 2026-10-24

### Fixed

- Data race reported by the race detector, and the regression fixes that followed the release.

## [0.0.12] - 2026-10-23

### Added

- Ghostboard page and the shared text boards behind it.
