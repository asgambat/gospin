---
title: "ADR-0007: Taskfile as the only build front-end"
status: "Accepted"
date: "2026-10-28"
authors: "Maintainers"
tags: ["architecture", "tooling", "build", "decision"]
supersedes: ""
superseded_by: ""
---

# ADR-0007: Taskfile as the only build front-end

## Status

**Accepted**

## Context

The repository used to carry both a `Makefile` and a `Taskfile.yml`, and they drifted: the Makefile
was the entry point documented in the README while the Taskfile held the Docker, version-bump and
race-detector tasks, and the artisanal targets (`tailwind-install`, `css`) still referred to a local
Tailwind build that no longer existed because the UI loads Tailwind from a CDN. Contributors had to
know which file owned which command, and CI had to stay consistent with both.

The project has no JavaScript toolchain — no `package.json`, no Node, no bundler, no CSS
compilation — so a build front-end only has to orchestrate `go` commands, `air`, `docker` and the
version bump.

## Decision

`Taskfile.yml` (go-task) is the only build front-end. The `Makefile` is deleted, and nothing in the
repository calls `make`.

- Every workflow is a task: `build` (lint + test + binary), `build:fast`, `build:cli`, `run`,
  `watch`, `test`, `test:verbose`, `test:race`, `test:cover`, `test:package`, `fmt`, `vet`,
  `staticcheck`, `lint`, `check`, `ci`, `deps*`, `clean`, `docker:*`, `release`, `version:show`,
  `version:bump`.
- Tasks depend on other tasks instead of duplicating shell lines: `build` calls `lint` and `test`,
  `check` calls `fmt`, `vet`, `lint`, `test`, and `ci` calls `deps`, `lint`, `test:race`,
  `build:fast`, so the local gate and the CI gate run the same definitions.
- Variables live in one `vars` block (`.build` output directory, binary names, `VERSION_FLAGS`,
  `LDFLAGS`, Docker image and tag) and are shell-evaluated at runtime, so the version comes from
  `git describe` rather than being pasted into a target.
- Prerequisites are declared where they can fail early: `lint` requires `golangci-lint`,
  `staticcheck` requires `staticcheck`, and `version:bump` requires an argument, each with an
  actionable message.
- File-based `sources`/`generates` pairs let go-task skip an up-to-date build without a hand-written
  dependency graph, and `platforms` covers the Windows-only clean commands.

Gates that do not belong in a task stay where they are: GitHub Actions runs `go vet`,
`staticcheck`, `golangci-lint` and `go test -race` directly with pinned versions, because a CI job
must not depend on a locally installed `task` to reproduce a failure.

## Consequences

### Positive

- **POS-001**: One command vocabulary for users, agents and documentation: every guide names `task
  ...`, and `task --list-all` is the discoverable index of what the repository can do.
- **POS-002**: Task dependencies remove the “which steps does `make build` run?” ambiguity — `task
  build` states that it runs lint and test first, and the same definitions compose into `check` and
  `ci`.
- **POS-003**: YAML with shell-evaluated variables makes cross-platform tasks (`{{exeExt}}`,
  `rm`/`rmdir` per platform) readable, which the Makefile handled with shell tricks and `.PHONY`
  lists.
- **POS-004**: Deleting the Makefile also deleted the dead Tailwind targets, so the documented build
  path matches what actually exists.

### Negative

- **NEG-001**: `task` is an extra prerequisite; contributors without it cannot build, while `make`
  ships with most Unix systems.
- **NEG-002**: The Taskfile is not used by CI, so a task can rot unnoticed until someone runs it
  locally — the pinned linter versions in the workflows and in the task descriptions must be kept in
  sync by hand.
- **NEG-003**: The build metadata flags now live in three places that must stay in lockstep
  (`Taskfile.yml`, `.air.toml`, `Dockerfile`), a duplication the single front-end cannot hide
  ([ADR-0004](0004-build-metadata-ldflags.md)).

## Alternatives Considered

### Keep both Makefile and Taskfile

- **ALT-001**: **Description**: Leave the Makefile as the documented entry point and the Taskfile for
  the richer tasks.
- **ALT-002**: **Rejection Reason**: Two front-ends drift by construction — that is how the dead
  Tailwind targets survived — and every new command has to be added twice or documented as missing
  from one of them.

### Makefile only

- **ALT-003**: **Description**: Fold the Docker and version tasks back into the Makefile and drop
  `task`.
- **ALT-004**: **Rejection Reason**: Version bumping, multi-arch builds and platform-conditional
  cleanups are considerably more verbose in make, and the resulting file mixes `.PHONY` bookkeeping
  with real logic.

### A shell script per workflow

- **ALT-005**: **Description**: Keep `scripts/build.sh`, `scripts/test.sh` and friends.
- **ALT-006**: **Rejection Reason**: No dependency graph, no `--list`, no cross-platform handling, and
  the scripts end up calling each other in an order nobody can see.

### Add a front-end bundler for the UI

- **ALT-007**: **Description**: Introduce Node, a bundler and a compiled Tailwind build to the task
  graph.
- **ALT-008**: **Rejection Reason**: The UI is plain HTML plus Alpine.js and htmx from CDNs with no
  assets to compile; a Node toolchain would add a lockfile, a CI job and a second language to a
  single-binary Go service for no functional gain.

## Implementation Notes

- **IMP-001**: `task install` installs only `air`, because the linters are documented as
  prerequisites with their pinned versions rather than silently fetched at build time.
- **IMP-002**: `build` and `build:cli` share `LDFLAGS`, so the server and `gb-cli` always report the
  same version ([ADR-0005](0005-gb-cli-separate-binary.md)).
- **IMP-003**: Hot reload is still `air` through `task watch`, configured by `.air.toml`, which builds
  to `.build/gospin` with the same flags.
- **IMP-004**: `AGENTS.md` and `docs/DEVELOPMENT.md` list the tasks rather than raw `go` commands;
  when a task is added, they are updated in the same change.

## References

- **REF-001**: [DEVELOPMENT.md - Build Process](../DEVELOPMENT.md#build-process)
- **REF-002**: [README.md - Development](../../README.md#development)
- **REF-003**: [ADR-0004](0004-build-metadata-ldflags.md)
