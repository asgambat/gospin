---
title: "ADR-0006: JSON data file with an in-memory cache and dirty tracking"
status: "Accepted"
date: "2026-10-28"
authors: "Maintainers"
tags: ["architecture", "persistence", "decision"]
supersedes: ""
superseded_by: ""
---

# ADR-0006: JSON data file with an in-memory cache and dirty tracking

## Status

**Accepted**

## Context

GoSpin stores containers, groups, schedules and their ordering. The state is small — tens of entries
on a home server — but it is written from the UI on every drag, toggle and edit, and read on every
page load and every scheduler tick. The deployment target is a single container on a Raspberry Pi
with a bind-mounted `config/` directory, and users are expected to inspect and hand-edit the state
file when the UI is not an option.

A database would add a server process or a C dependency, a schema and a migration story for a
document that fits in a few kilobytes. Writing the file on every request would flash the SD card and
serialise the handlers behind disk I/O.

## Decision

The data file (`data.file_path`, by default `config/data/config.json`) is the source of truth on
disk, and `internal/cache.Store` keeps the whole document in memory for the lifetime of the process.
`internal/repository.JSONRepository` owns the disk side.

- **Startup.** `main` loads the document through the repository (`Load`), which decodes the JSON,
  applies defaults for missing pointers and empty slices, drops schedules whose target container or
  group no longer exists, and validates the result with `go-playground/validator`. A failure is fatal
  (`cannot load data file`), because starting with an empty document would let the first save
  overwrite a file the user can still repair. The cache is then built from that document.
- **Mutations are memory-only.** Every controller mutation goes through a store method
  (`AddContainer`, `RemoveContainer`, `AddGroup`, `RemoveGroup`, `AddSchedule`, `RemoveSchedule`),
  which mutates under a `RWMutex`, marks the cache dirty, and returns a deep copy so callers never
  share slices with the store. A deep copy is a JSON round-trip, which also keeps pointer fields
  (`running`, `active`, `favorite`, `pinned`) from being aliased. Deletions cascade in memory:
  removing a container removes its schedules and its group memberships.
- **Persistence is periodic and conditional.** `cache.StartPersistenceScheduler` ticks on
  `data.persist_interval_secs` (default 5) and writes only when the dirty flag is set: it takes a
  snapshot, stamps `metadata.lastUpdate` with `time.Now().UnixMilli()`, saves, clears the flag and
  records the new timestamp. On shutdown it performs one last flush with the cancellation detached
  (`context.WithoutCancel` plus a 30-second timeout), and `App.Shutdown` waits for the goroutine
  through `persistDone`, so a container stop does not lose the last edits.
- **Writes are atomic.** `Save` validates, marshals indented JSON to a temporary file in the same
  directory, `Sync`s it, closes it and renames it over the data file. A crash mid-write leaves the
  previous document intact instead of a truncated one.
- **The file is also an input.** `StartWatcher` watches the parent directory with `fsnotify` (not the
  file, so a temp+rename is still observed), filters events by basename, debounces them by 200 ms and
  then reloads. A reload is skipped when the disk timestamp is older than the cache, and when the
  cache is dirty — the cache is about to win anyway. Equal timestamps trigger a content comparison
  (`AreDataDocumentsEqual`, metadata excluded) so a no-op touch does not replace live state.

## Consequences

### Positive

- **POS-001**: No database process, no CGo, no schema and no migrations: a Go binary, a JSON file and
  a bind mount are the whole persistence layer.
- **POS-002**: The document stays human-readable and hand-editable, which is what makes recovery from
  a bad batch of edits practical on a device without a database client.
- **POS-003**: Reads and UI writes never touch the disk, so a page render or a toggle costs a mutex
  and a copy, and the SD card sees one write per interval instead of one per action.
- **POS-004**: `metadata.lastUpdate` gives a cheap optimistic check that prevents the watcher from
  clobbering newer in-memory state, and it is the only piece of persistence metadata the format needs.

### Negative

- **NEG-001**: Crash-safety is bounded by the write interval: up to `data.persist_interval_secs` of
  accepted edits are lost if the process dies between ticks.
- **NEG-002**: Every flush rewrites the whole document, so cost grows with the number of entries
  rather than with the size of the change.
- **NEG-003**: The design assumes one writer process. Two instances sharing the data file would
  interleave whole-document writes and the timestamp comparison would not detect the conflict.
- **NEG-004**: The dirty flag is document-wide, so one edit causes one full rewrite of every section,
  and there is no per-section change tracking to build on later.

## Alternatives Considered

### SQLite (or bbolt)

- **ALT-001**: **Description**: Store containers, groups and schedules in tables and let the database
  handle durability, concurrency and migrations.
- **ALT-002**: **Rejection Reason**: Adds a schema, migration code and either CGo or a pure-Go engine
  for a document measured in kilobytes, and loses the ability to fix the state with an editor. The
  registry is a document, not a relational workload.

### Write-through on every mutation

- **ALT-003**: **Description**: Save the document inside each store method.
- **ALT-004**: **Rejection Reason**: Every toggle would block on `fsync`, the handlers would need
  rollback when the write fails, and the SD-card wear on a Pi becomes a real concern. Dirty tracking
  keeps the failure handling in one place, the persistence goroutine.

### In-memory only, rebuilt from Docker on start

- **ALT-005**: **Description**: Treat the container runtime as the source of truth and keep only the
  curated metadata in the file.
- **ALT-006**: **Rejection Reason**: The runtime does not know about friendly names, URLs, groups,
  favourites, pinning or schedules; those are exactly what the user maintains.

### Accept only restarts instead of watching the file

- **ALT-007**: **Description**: Drop `fsnotify` and require a restart after a manual edit.
- **ALT-008**: **Rejection Reason**: Hand-editing the file is a documented recovery path, and a
  restart also restarts every schedule evaluation. The watcher costs one goroutine and a debounce.

## Implementation Notes

- **IMP-001**: `repository.Saver` and the small `PersistableStore` interface let the persistence
  scheduler depend on the two methods it needs (`IsDirty`, `Snapshot`, `ClearDirty`, `SetLastUpdate`)
  instead of the whole store.
- **IMP-002**: Validation runs both on load and before save, so an invalid in-memory document is never
  written, and the store stays the only mutation point that can produce one.
- **IMP-003**: The ghostboard keeps its own file (`ghostboard.json`) with the same periodic-flush
  shape, so a board write never rewrites the container registry ([ADR-0002](0002-ghostboard-sse.md))
  and a registry flush never touches board text.
- **IMP-004**: Success is observable in the log: `cache persisted to disk` at info level per flush and
  `cache reloaded from newer disk version` when the watcher wins.

## References

- **REF-001**: [ARCHITECTURE.md - Cache Store Architecture](../ARCHITECTURE.md#cache-store-architecture)
- **REF-002**: [README.md - Configuration](../../README.md#configuration)
- **REF-003**: `internal/cache/store.go`, `internal/cache/scheduler.go`, `internal/repository/json_repository.go`
