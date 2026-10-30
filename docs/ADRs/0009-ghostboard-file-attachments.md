---
title: "ADR-0009: Board-scoped file attachments, always served as downloads"
status: "Accepted"
date: "2026-10-29"
authors: "Maintainers"
tags: ["architecture", "ghostboard", "files", "storage", "decision"]
supersedes: ""
superseded_by: ""
---

# ADR-0009: Board-scoped file attachments, always served as downloads

## Status

**Accepted**

## Context

The ghostboard moves text between devices with no login, no account and no upload step (ADR-0002),
and its PIN protects the boards that need one (ADR-0008). Text alone, however, does not cover the
actual use: a screenshot, a PDF or an archive still needs another tool, which is the reason the
boards are used at all — moving something between a phone and a laptop on the same network.

Attachments bring in questions the text never had: where the bytes live, who may read them, what
happens to them when the board is cleared, how much disk one client can take, and what a browser
does with a file the server hands back. The constraints are the ones of the rest of the store: one
binary, no database, a `ghostboard.json` document that is already flushed every couple of seconds,
a 1-second per-request timeout on the JSON API and an instance that may run on a Raspberry Pi with a
small disk.

## Decision

An attachment belongs to a board and shares its life. The decision is carried by
`internal/ghostboard/files.go` (storage, quotas, reconciliation), the `files` routes in
`internal/api/route/ghostboard_route.go` and the JSON/plain handlers in
`internal/api/controller/ghostboard_files.go`.

- **Board scope, no global list.** Files are addressed as `/ghostboard/:board/files/:id`. There is no
  "all files" page and no cross-board browsing: a board is the unit of sharing, and a file is
  reachable exactly where its text is.
- **Metadata in `ghostboard.json`.** The `files` array of a board is part of the document the store
  already flushes with dirty tracking (ADR-0006), so a board's text and its attachments are one
  consistent unit and no second persistence mechanism was needed. The bytes live next to the file,
  in `<dir of ghostboard.file_path>/files/<board>/<id>`.
- **Random ids, not names, on disk.** An id is 128 random bits in lowercase base32 (26 characters).
  The uploader's name is kept as a download label only, sanitized before it reaches the metadata, a
  header or the page, so no user string ever takes part in a path.
- **One file per request, streamed.** A multi-file drop is a loop of requests, which keeps each
  transfer's progress and cancel independent and lets the server write straight to disk: the body is
  streamed to a temporary file, and the quota check, the rename and the metadata append happen
  together under the store mutex, so the temporary file is the reservation.
- **Always a download.** Every response is `application/octet-stream` with
  `Content-Disposition: attachment`, `nosniff`, a sandbox CSP and no inline preview. An uploaded HTML
  or SVG document must not be able to run on the dashboard origin, where it would carry the PIN
  session of a private board.
- **The board's lifecycle is the file's lifecycle.** An upload or a delete counts as board activity
  and re-arms the idle clear, the idle clear deletes text and attachments together, and
  `file_ttl_secs` is a hard cap from the upload for the boards kept alive by an open page.
- **Fixed, configurable limits.** `max_file_bytes`, `max_board_file_bytes`, `max_total_file_bytes`
  and `max_file_count` are enforced server-side, with the page refusing an oversized file first.
- **Metadata and disk are reconciled at startup**: a file without metadata, metadata without a file,
  an unknown board directory, a leftover temporary upload and a symlink are all removed, the symlink
  itself and never its target.

## Consequences

### Positive

- **POS-001**: A board becomes a complete transfer surface — text, links and files — without an
  account system, a database or a new service to run.
- **POS-002**: The PIN of a private board covers its attachments automatically, because the file
  routes sit behind the same middleware on both API surfaces.
- **POS-003**: Quotas bound what a single client can consume, and `file_ttl_secs` bounds it in time,
  so an instance left open does not fill its disk with forgotten uploads.
- **POS-004**: Streaming both ways keeps an 800 MiB upload out of memory, and `Range` on the download
  lets a large file resume.
- **POS-005**: Nothing new is persisted: a crash can only lose the metadata of the last seconds, and
  the next startup removes the orphan bytes.

### Negative

- **NEG-001**: A temporary file of the full size of an upload exists next to the final one until the
  commit, so the free space must exceed the largest allowed upload.
- **NEG-002**: With the default `clear_delay_secs` of 300 seconds, files usually disappear about five
  minutes after the last activity, which surprises users who read the 24-hour TTL as the lifetime.
  The README states the interaction explicitly.
- **NEG-003**: There is no resumable upload and no deduplication: a retry of a large upload starts
  from zero, and the same file twice costs twice the disk.
- **NEG-004**: A reverse proxy in front of GoSpin needs its own body limit raised, which is a
  deployment step the text-only feature never had.

## Alternatives Considered

### A global file area with an index page

- **ALT-001**: **Description**: one `/files` area for the whole instance, with a list, a search and no
  relation to a board.
- **ALT-002**: **Rejection Reason**: it needs its own naming, permissions and cleanup policy, and it
  breaks the model where the board is the unit of sharing — a file would outlive the text it belongs
  to and stay readable after the board is gone.

### Serving files inline, with previews

- **ALT-003**: **Description**: send the detected content type and render images or PDFs in the page.
- **ALT-004**: **Rejection Reason**: inline serving turns an uploaded document into content of the
  dashboard origin, where it would run with the PIN cookie of a private board. `attachment` plus
  `nosniff` keeps the feature safe by construction; thumbnails would need transcoding and a cache.

### Storing the metadata in a separate file

- **ALT-005**: **Description**: a `files.json` next to the boards, written by its own loop.
- **ALT-006**: **Rejection Reason**: two documents describing one board can disagree after a crash or a
  concurrent write, and reconciling them costs more code than reusing the flush loop of the store.

### Buffering the upload to honour the 1-second request timeout

- **ALT-007**: **Description**: keep every route on the existing per-request timeout and read the body
  into memory before storing it.
- **ALT-008**: **Rejection Reason**: an 800 MiB upload cannot fit a one-second budget or the process
  memory. The handlers set their own transfer deadline instead, which is explicit and configurable
  through `file_transfer_timeout_secs` (`SetReadDeadline`/`SetWriteDeadline` on the response
  controller).

### A separate PIN for the attachments

- **ALT-009**: **Description**: files protected by their own secret, independent of the board text.
- **ALT-010**: **Rejection Reason**: a second secret to distribute and a second session to expire, for
  no gain: whoever can read the text of a private board has no reason to be denied its files.

## Implementation Notes

- **IMP-001**: `internal/ghostboard/files.go` owns `FileConfig` (limits, root, TTL, transfer timeout),
  `WriteFile`, `OpenFile`, `Files`, `DeleteFile`, `DeleteFiles`, `SweepFiles` and `ReconcileFiles`.
  The store configuration is assembled in `internal/app/app.go`; the limits come from the
  `ghostboard.max_file_bytes`, `max_file_count`, `max_board_file_bytes`, `max_total_file_bytes`,
  `file_ttl_secs`, `file_transfer_timeout_secs` and `files_enabled` keys.
- **IMP-002**: `internal/api/controller/ghostboard_files.go` maps store errors to `400`, `404`, `409`,
  `413`, `415`, `507` and `504` with the limiting key in the body; `internal/api/route` registers both
  surfaces behind `GhostboardPIN` and without the JSON timeout middleware.
- **IMP-003**: Tests: `internal/ghostboard/files_test.go` (quotas, TTL, idle clear, reconciliation,
  persistence), `internal/api/controller/ghostboard_files_test.go` (handlers, headers, statuses),
  `internal/api/route/ghostboard_files_route_test.go` (registration, PIN gate, disabled feature),
  `internal/boardclient/files_test.go` and `cmd/board/files_test.go` (CLI), plus a Playwright pass
  over the panel.
- **IMP-004**: The page reads `data-max-file-bytes` injected by `serveGhostboardPage`, uses XHR for
  progress and cancel, and renders names with `x-text` and `:download` only.

## References

- **REF-001**: ADR-0002 (ghostboard over Server-Sent Events), ADR-0006 (JSON persistence with dirty
  tracking), ADR-0008 (PIN protection for private ghostboards).
- **REF-002**: README, sections "Ghostboard", "File attachments" and "Ghostboard configuration".
- **REF-003**: `docs/ARCHITECTURE.md` (Ghostboard Store), `docs/FRONTEND.md`, `docs/CLI.md`.
- **REF-004**: `docs/plans/board-uploads-spec.md`, the specification this decision implements.
