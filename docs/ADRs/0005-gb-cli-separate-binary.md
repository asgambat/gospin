---
title: "ADR-0005: gb-cli as a separate binary with its own configuration"
status: "Accepted"
date: "2026-10-28"
authors: "Maintainers"
tags: ["architecture", "cli", "decision"]
supersedes: ""
superseded_by: ""
---

# ADR-0005: gb-cli as a separate binary with its own configuration

## Status

**Accepted**

## Context

Ghostboards are plain text, so they are useful from shell scripts: CI jobs post build output, an
operator appends a note from a terminal, a cron job reads a board back. Reaching the boards with
`curl` works but pushes JSON quoting, board name validation, retries and configuration onto every
script. The server must keep working without the CLI, and the CLI must work against a remote server
without touching the server files.

## Decision

`gb-cli` ships as a second binary, `cmd/board`, built by `task build:cli` with the same `-ldflags`
metadata as the server. It is a thin HTTP client implemented in `internal/boardclient`:

- reads and writes go through the plain-text API `/api/boards/:board`, which stores the body byte for
  byte and answers `204` on a write, so a script pipes text in and out without JSON;
- `read --stat`/`--json` use `/ghostboard/:board` because only that route carries the revision and the
  update time, and `follow` consumes `/ghostboard/:board/events`;
- configuration is resolved in one place — defaults, then a YAML file, then `GO_SPIN_BOARD_*`
  environment variables, then flags — and `Config.Validate` runs on the effective values, so a flag
  corrects a mistyped file entry;
- failures are typed (`EndpointError`, `BoardNameError`, `misuseError`) and map to dedicated exit codes
  `3` (the URL does not expose the board API), `4` (invalid board name) and `2` (command-line misuse),
  so a script can tell a wrong URL from a wrong name from a typo.

## Consequences

### Positive

- **POS-001**: Scripts get a CLI with board-name validation, retries, headers, a Bearer token and
  stable exit codes instead of ad-hoc `curl` invocations.
- **POS-002**: The client needs no filesystem access and works against a local, containerised or
  remote GoSpin, so it can live on an operator's laptop while the server stays elsewhere.
- **POS-003**: The plain-text surface keeps the common case (pipe in, pipe out) free of JSON, and the
  JSON surface exists only where a script needs the revision.

### Negative

- **NEG-001**: A second binary to build, version and distribute; `task build:cli` and the release
  process must keep including it.
- **NEG-002**: `add` is a read followed by a write, so a concurrent writer in between can be
  overwritten; `set` is the primitive that guarantees a final content.
- **NEG-003**: The CLI configuration mirrors the server configuration in name only; the two must be
  documented apart.

## Alternatives Considered

### Document `curl` recipes only

- **ALT-001**: **Description**: Keep the board API and let users write their own requests.
- **ALT-002**: **Rejection Reason**: Every script would reimplement quoting, UTF-8 validation, retry
  and the exit-code contract, and `follow` — a long-lived stream — is impractical with `curl` alone.

### Subcommand of the server binary

- **ALT-003**: **Description**: Ship `gospin board ...` in the server binary.
- **ALT-004**: **Rejection Reason**: The client would then carry the Docker SDK and the whole server
  dependency tree, and installing the server container would be the only way to get a client.

### Separate repository

- **ALT-005**: **Description**: Publish the client as its own project.
- **ALT-006**: **Rejection Reason**: The client has to track the API in lockstep with the server; two
  repositories would need version negotiation for a single-operator tool.

## Implementation Notes

- **IMP-001**: The help, `version`, `config` and `completion` never contact the server, so
  misconfiguration is diagnosable offline; `config --paths` lists every candidate file.
- **IMP-002**: Text and JSON never share stdout, which is why `download -` and `follow` refuse
  `--json` while `read --json` refuses `--stat`.
- **IMP-003**: `gb-cli version` reports the injected metadata; `go run ./cmd/board version` reports the
  compiled defaults, which is expected during development.

## References

- **REF-001**: [CLI.md](../CLI.md)
- **REF-002**: [ARCHITECTURE.md - Ghostboard Store](../ARCHITECTURE.md#ghostboard-store)
- **REF-003**: [ADR-0004](0004-build-metadata-ldflags.md)
