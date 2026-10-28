---
title: "ADR-0002: Server-Sent Events for the ghostboard"
status: "Accepted"
date: "2026-10-28"
authors: "Maintainers"
tags: ["architecture", "ghostboard", "realtime", "decision"]
supersedes: ""
superseded_by: ""
---

# ADR-0002: Server-Sent Events for the ghostboard

## Status

**Accepted**

## Context

The ghostboard is a shared text board: every browser on `/ui/ghostboard/<board>` sees the same text
and every edit reaches the other open pages within a fraction of a second. Boards hold plain text,
are written by browsers and by `gb-cli`, must survive a restart, and are deliberately
unauthenticated. The traffic is one shared document that the server distributes; clients only send
whole replacements.

## Decision

The server keeps every board in memory in `internal/ghostboard.Store` (text, revision, last update
time, subscribers, idle-clear timer) and exposes a Server-Sent Events stream at
`/ghostboard/:board/events` (`internal/api/route/ghostboard_route.go`).

- Each write increments a monotonic revision and fans an `Event` out to every subscriber. Subscribers
  hold a buffered channel of 16 events: a slow stream drops intermediate events and converges on the
  next snapshot, and every event carries the full text so a dropped intermediate state is harmless.
- The stream starts with a snapshot, then sends each change plus a keep-alive comment every
  `ghostboard.heartbeat_interval_secs`. It bypasses gzip and the per-request timeout and clears the
  server write deadline.
- The text is cleared after `ghostboard.clear_delay_secs` from the last write or the last subscriber
  leaving, whichever is last; a generation counter prevents a timer that fires during a write from
  emptying fresh text. Dirty boards are flushed to their own JSON file every
  `ghostboard.persist_interval_secs` and once more on shutdown.
- The browser sends its tab id in `X-Ghostboard-Client`, which comes back on the event, so a tab
  ignores the echo of its own write and the caret does not jump.

## Consequences

### Positive

- **POS-001**: The browser reconnects natively: `EventSource` retries and the next snapshot repairs
  any gap, so no custom reconnection protocol is needed.
- **POS-002**: Only the server pushes; the client sends whole replacements over plain `POST`, which
  keeps the protocol small and works with `curl` and `gb-cli`.
- **POS-003**: Server-Sent Events are plain HTTP with the standard library, so the feature adds no
  dependency and works through the existing router, middleware and reverse proxies.

### Negative

- **NEG-001**: One long-lived connection per open tab per board; many viewers cost many connections.
- **NEG-002**: The transport is one-way, so a client that needs a reply still performs a separate
  `POST`.
- **NEG-003**: There is no authentication and no per-board access control: anyone who can reach the
  server can read and rewrite every board.

## Alternatives Considered

### WebSocket

- **ALT-001**: **Description**: A bidirectional socket carrying text deltas and acknowledgements.
- **ALT-002**: **Rejection Reason**: The application has no server-to-client request and no
  low-latency client-to-server path that `POST` does not already cover; a WebSocket adds framing,
  ping/pong handling and proxy configuration for no functional gain.

### Periodic polling

- **ALT-003**: **Description**: The page polls `/ghostboard/:board` every few seconds.
- **ALT-004**: **Rejection Reason**: Latency would be bound to the poll interval, and every open tab
  would generate a request per interval regardless of whether anything changed.

### Long polling

- **ALT-005**: **Description**: Hold a request open until the revision changes, then answer and
  reopen.
- **ALT-006**: **Rejection Reason**: Reimplements what `EventSource` provides, including the
  reconnection loop, and multiplies the number of requests during quiet periods.

## Implementation Notes

- **IMP-001**: `Update` is a no-op when the text is unchanged, so a typing pause does not bump the
  revision or wake every subscriber.
- **IMP-002**: The JSON persistence file is separate from the container registry, so a board flush
  never rewrites the data document.
- **IMP-003**: The board name is validated before the store is touched, and the request body limit is
  derived from `ghostboard.max_text_bytes` so a payload of escapes or percent-encoded forms is not
  rejected while its decoded text is within the limit.

## References

- **REF-001**: [ARCHITECTURE.md - Ghostboard Store](../ARCHITECTURE.md#ghostboard-store)
- **REF-002**: [README.md - Ghostboard](../../README.md#ghostboard)
- **REF-003**: [ADR-0005](0005-gb-cli-separate-binary.md) for the command-line client of the same API
