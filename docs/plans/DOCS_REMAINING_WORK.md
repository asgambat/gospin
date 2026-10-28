# Documentation Follow-up Plan

Status: open, awaiting maintainer decisions
Date: 2026-10-28

## Context

An audit of the documentation set found no broken links and no wrong API or configuration
references. What remained was content that could not be fixed without a decision, because each item
changes user-visible material, deletes tracked files, or needs an environment this repository does
not provide. The items that were unambiguous (build prerequisites, environment variable layout,
README tree and features, homepage and Cosmos in the architecture guide, emoji-free headings,
documentation index, branch-document rename, Capgemini footers, compose port, AGENTS typos) are
already fixed, and `CHANGELOG.md` now exists.

## Open items

### 1. Screenshots

**Done.** `docs/screenshots/` now holds `containers.png`, `groups.png`, `schedules.png`, `home.png` and
`ghostboard.png`, captured from a demo instance (memory runtime, fictional containers and hosts) and
linked from the README with descriptive alt text. `task screenshots` (`scripts/screenshots/`)
regenerates all five: it builds the server, serves the fixtures in `scripts/screenshots/fixtures/`
and drives Playwright's Chromium, so no manual sequence is left to remember.

### 2. ADR backfill

**Done.** Seven records describe the load-bearing decisions, derived from the current code:
[0001 polling scheduler](../ADRs/0001-polling-scheduler.md),
[0002 ghostboard over SSE](../ADRs/0002-ghostboard-sse.md),
[0003 homepage and actions proxy](../ADRs/0003-homepage-yaml-actions-proxy.md),
[0004 build metadata and version bump](../ADRs/0004-build-metadata-ldflags.md),
[0005 gb-cli as a separate binary](../ADRs/0005-gb-cli-separate-binary.md),
[0006 JSON data file with dirty tracking](../ADRs/0006-json-persistence-dirty-cache.md) and
[0007 Taskfile as the only build front-end](../ADRs/0007-taskfile-build-frontend.md). They are
listed in `docs/README.md`.

### 3. Archived plan in Italian

**Problem.** `docs/plans/archive/HTMX_MIGRATION_PLAN.md` is the only non-English document.
`AGENTS.md` names it as the single exception.

**Options.** Keep as a historical record; translate it; delete it.

**Acceptance.** The exception is either documented or gone.

### 4. `docs/CLI.md`

**Done.** The full `gb-cli` reference moved to [CLI.md](../CLI.md): commands, examples, output modes,
exit codes, configuration resolution, environment variables and the server endpoints it uses. The
README keeps a short section with five examples and a link.

### 5. Slim the document templates

**Done.** The three templates were trimmed to the sections this repository uses: the ADR template
lost its `authors`/`tags`/`supersedes` front-matter, the PRD template lost the revision-history,
stakeholder-contact, timeline, KPI, compliance and open-questions sections, and the plan template
lost the deployment/rollback, monitoring, compliance, communication, appendix and AI validation
rules, keeping scope, task list and verification.

### 6. Stray configuration backup

**Done.** `config/homepage.yaml_bk` was deleted. It was a stale copy of `homepage.yaml` — older
services and bookmarks, plus a `settings.version` key that no struct reads — and nothing referenced
it. `config/homepage.yaml` stays the example users copy and edit.

**Acceptance.** No unreferenced `*_bk` file in `config/` — met.

### 7. Image namespace on GHCR

**Problem.** Docker Hub publishes `bassista/gospin` through `secrets.DOCKERHUB_USERNAME`, while GHCR
derives its name from the repository and publishes `ghcr.io/asgambat/gospin`. Two names for the same
image.

**Options.** Accept both and document which one each registry serves; or pin a single name where the
registry allows it. Also worth confirming that the Docker Hub secret points at an account with push
access to `bassista/gospin`.

**Acceptance.** The README states the exact image reference for each registry, and the release
workflow pushes to both without manual edits.

### 8. README vs ARCHITECTURE overlap

**Done.** The README keeps the project tree and a list of links; the patterns, data flow,
[concurrency model](../ARCHITECTURE.md#concurrency-model),
[key design decisions](../ARCHITECTURE.md#key-design-decisions) and
[performance and resources](../ARCHITECTURE.md#performance-and-resources) now live only in
`docs/ARCHITECTURE.md`, which gained sections for the last three. The performance and resource
figures were replaced by measurements taken on the demo instance (memory runtime, Raspberry Pi 4)
and by figures derived from the code (poll and persist intervals) instead of being carried over.

**Acceptance.** Each fact has one home, and the README links to it — met.

## Sequencing

Item 3 is small and can go first. Item 7 needs a decision. Items 1, 2, 4, 5, 6 and 8 are closed.
