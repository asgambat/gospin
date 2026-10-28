---
title: "ADR-0004: Build metadata injected with -ldflags from one bump script"
status: "Accepted"
date: "2026-10-28"
authors: "Maintainers"
tags: ["build", "release", "decision"]
supersedes: ""
superseded_by: ""
---

# ADR-0004: Build metadata injected with -ldflags from one bump script

## Status

**Accepted**

## Context

The version string appears in the dashboard footer and in `gb-cli version`, and users compare it with
the image they pulled or the binary they built. The metadata has to describe the binary that is
running, not a file that may have been edited afterwards. GoSpin is built from four places — `task
build`, `task build:fast`/`build:cli`, `air` during development and the `Dockerfile` in CI — and every
one of them must produce the same values for the same commit. Releases are git tags, and publishing
the image happens from the tag in CI.

## Decision

Four variables in `internal/version` (`Version`, `BuildTime`, `GitCommit`, `GoVersion`) are populated
at link time with `-ldflags "-X ..."`, with `git describe --tags --always --dirty` as the version
source and `cat .version` as the fallback when no tag exists.

A single script, `.task/version_bump.sh` (`task version:bump -- X.Y.Z`), rewrites every version-bearing
file — `.version`, `internal/version/version.go`, `Taskfile.yml`, `Dockerfile`, `README.md` and both
Docker workflows — validates the input against the current version and the existing tags, then commits
and tags `vX.Y.Z`. Pushing that tag is what publishes the image.

## Consequences

### Positive

- **POS-001**: The running binary carries its own version, commit and build time; nothing can go stale
  after the build.
- **POS-002**: Server and CLI share the same flags and therefore never disagree about the version.
- **POS-003**: A single script owns every version-bearing file, so a bump cannot update one file and
  miss another.

### Negative

- **NEG-001**: The `-X` block is duplicated in the Taskfile, the `Dockerfile` and the two `air`
  configurations, and the copies must be kept in lockstep by hand.
- **NEG-002**: Changing the version requires a new build; there is no runtime override, by design.
- **NEG-003**: The bump script edits files with pattern matching, so a renamed build file would need
  the script updated in the same change.

## Alternatives Considered

### Read `.version` at runtime

- **ALT-001**: **Description**: Ship the file next to the binary and read it at startup.
- **ALT-002**: **Rejection Reason**: Container images copy only the binary, and a file next to a
  binary can be edited by anyone with write access, which defeats the point of a build stamp.

### Embed the version with `go:embed`

- **ALT-003**: **Description**: Embed a generated file into the binary.
- **ALT-004**: **Rejection Reason**: It still needs a generation step before `go build`, and `-X` is
  the standard mechanism that works with `go build`, `go run` and the Docker build without extra
  tooling.

### Derive the version from `debug.ReadBuildInfo`

- **ALT-005**: **Description**: Use the VCS information Go embeds when building a module.
- **ALT-006**: **Rejection Reason**: It reports the module version and the commit only for builds made
  by the module toolchain with VCS stamping enabled, gives no build time, and is empty for a plain
  `go build` in a dirty tree — exactly the situation the footer has to describe.

## Implementation Notes

- **IMP-001**: Both binaries are built with the same `VERSION_FLAGS`, so `gb-cli version` and the
  dashboard footer agree.
- **IMP-002**: `task version:bump` refuses a value that is not `MAJOR.MINOR.PATCH`, is lower than or
  equal to the current one, already has a tag, or disagrees with the compiled default.
- **IMP-003**: `latest` follows every `0.x` release and, from `1.0.0`, only a minor or major tag;
  pushes to `main` publish the `main` tag without moving `latest`.

## References

- **REF-001**: [AGENTS.md - Versioning and Releases](../../AGENTS.md)
- **REF-002**: [README.md - Releasing a New Version](../../README.md#releasing-a-new-version)
- **REF-003**: `.task/version_bump.sh`
