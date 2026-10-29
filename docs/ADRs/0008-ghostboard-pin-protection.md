---
title: "ADR-0008: PIN protection for private ghostboards"
status: "Accepted"
date: "2026-10-29"
authors: "Maintainers"
tags: ["architecture", "ghostboard", "security", "decision"]
supersedes: ""
superseded_by: ""
---

# ADR-0008: PIN protection for private ghostboards

## Status

**Accepted**

## Context

Ghostboards are deliberately unauthenticated (ADR-0002): anyone who can reach the server reads and
rewrites every board. That is the right default for a homelab dashboard, but it rules out the use
that made the boards useful in the first place — a scratch space for a work in progress, a note
with a hostname or a credential-free build log that should not be readable by whoever lands on
`/ui/ghostboard/lavoro`.

The constraint is proportionality. GoSpin has no users, no sessions and no database, it runs as a
single binary behind a reverse proxy, and its whole value is that it stays out of the way. Adding an
account system, a login page and a user store would be a bigger feature than the dashboard it
protects. At the same time a bare 4-digit PIN over a network is only meaningful with throttling,
and a PIN typed into a form is only meaningful over HTTPS.

Three questions had to be answered: where the "this board is private" flag lives, how the PIN
travels, and what is stored on the server.

## Decision

One shared PIN — set in configuration, never per board — protects the boards named in
`ghostboard.private_boards`, for reading and writing alike (`internal/ghostboard/auth.go`,
`internal/api/middleware/ghostboard_auth.go`).

- **The flag is configuration, and the default is public.** A board is private because its name is
  in `private_boards`; every other board keeps today's behaviour. The list is validated at startup,
  so a board becomes private before its first write and no lock state needs to be persisted in
  `ghostboard.json`. The rejected alternative — private by default with an explicit public list —
  protects a new board automatically but surprises the common case, where a board is just a
  scratch pad.
- **The PIN is stored as `HMAC-SHA256(pin_secret, pin)`** in `ghostboard.pin_hmac`, with the secret
  from `GO_SPIN_GHOSTBOARD_PIN_SECRET`. A 4-digit PIN has 10,000 combinations, so a hash alone would
  be brute-forced in milliseconds from a copied configuration file; the secret is what makes the
  digest useless on its own. `gb-cli pin-hash` prints the value.
- **The PIN is traded for a stateless session token** by `POST /ghostboard/:board/unlock`
  (`v1.<expiry>.<hmac>`). The browser gets it as an HttpOnly, `SameSite=Strict` cookie, scripts get
  it in the response body and send it as `Authorization: Bearer`. The token carries its expiry and
  its signature, so a restart keeps the open sessions alive and nothing has to be stored.
- **Failures are throttled per client address**, `pin_max_attempts` before a `pin_lockout_secs`
  lockout that doubles on every further batch of failures, up to an hour. The counter is keyed on
  the connection address, never on `X-Forwarded-For`, which the client controls.
- **The PIN is accepted as 4-64 characters.** Four digits stay the short, familiar form; a
  passphrase costs nothing because the digest does not care about length.

## Consequences

### Positive

- **POS-001**: A private board is protected on every surface at once — JSON API, plain-text API,
  event stream and the web page — because the gate is one middleware on the route group.
- **POS-002**: No new state: the protection is configuration, so a restart, a cleared board or a
  pruned persistence file cannot leave a board silently open.
- **POS-003**: The PIN travels once per session instead of once per request, so it does not end up
  in request logs, in the browser history or in a proxy access log.
- **POS-004**: `gb-cli` needs no new interaction model: an unlock happens by itself on the first
  `401`, and a rejected PIN is never retried.

### Negative

- **NEG-001**: The PIN is shared by every board in the list: there is no per-board secret, no
  per-person revocation and no way to tell who read a board. That is accepted for a single-user
  homelab tool; anything finer needs real authentication.
- **NEG-002**: This is access control, not encryption. The text stays in plain text in
  `ghostboard.json`, so anyone with access to the host reads every board regardless of its PIN.
- **NEG-003**: A 4-digit PIN is weak by construction; the throttling is what makes it usable. A
  deployment that disables the lockout or ignores HTTPS has no protection left.
- **NEG-004**: Changing the secret invalidates every digest and every open session at once, and an
  expired session brings the lock form back while the page is open (the text typed locally is kept
  and written back after unlocking).

## Alternatives Considered

### The PIN in a header on every request

- **ALT-001**: **Description**: `X-Ghostboard-Pin` on each call, with the PIN in the query string
  for the event stream, since `EventSource` cannot set a header.
- **ALT-002**: **Rejection Reason**: The PIN lands in the access logs and in the browser history,
  and `navigator.sendBeacon` — used to flush the last edit when a tab closes — cannot attach a
  header at all, so the flush would stop working on protected boards.

### Boards protected by a dedicated URL prefix

- **ALT-001**: **Description**: Serve private boards under `/ui/boards-private/<board>`, deriving
  the flag from the path instead of from configuration.
- **ALT-002**: **Rejection Reason**: A path is a namespace, not a gate: the public path would still
  reach the same board unless the server already knows it is private, which is exactly the state
  this avoids. It would also duplicate routing, the PWA scope and the service-worker cache, and
  split every board into two names.

### A per-board PIN stored in the persistence file

- **ALT-001**: **Description**: Lock a board from the UI, keep the digest in `ghostboard.json`, and
  let whoever knows the current PIN change it.
- **ALT-002**: **Rejection Reason**: It needs migration, an owner concept ("who may lock a board?")
  and a race at first write, for a feature whose whole point is a shared secret on a homelab tool.

### HTTP Basic authentication on the protected routes

- **ALT-001**: **Description**: One credential in the configuration, checked by the middleware;
  browsers prompt, `curl -u` works, no UI work at all.
- **ALT-002**: **Rejection Reason**: It is a legitimate alternative and less code, but the browser
  dialog is per-host rather than per-board, there is no logout, and the credential is resent on
  every request instead of once per session. The PIN gate keeps the page usable on a phone.

### Encrypting the board text at rest

- **ALT-001**: **Description**: Encrypt `ghostboard.json` with a key from the environment, and derive
  nothing from the PIN.
- **ALT-002**: **Rejection Reason**: It answers a different threat model (someone reading the file)
  and cannot be keyed by a 4-digit PIN. It remains possible as a separate feature; the PIN must not
  be presented as encryption.

## Implementation Notes

- **IMP-001**: `internal/ghostboard/auth.go` holds the digest, the token format, the attempt limiter
  and `Authenticator.IsPrivate`. The gate itself is
  `middleware.GhostboardPIN` (`internal/api/middleware/ghostboard_auth.go`); a `nil` authenticator
  means no PIN is configured and every board is public.
- **IMP-002**: `internal/app` builds the authenticator from `internal/config`, so an invalid
  `pin_hmac` or a missing secret stops the server at startup instead of leaving a board exposed
  while looking protected.
- **IMP-003**: The page loads the board state before opening the event stream
  (`ui/assets/ghostboard.js`): a `401` shows the PIN form, and `POST /ghostboard/:board/unlock` is
  the only way out of it. A `401` during a save brings the form back without discarding the local
  text.
- **IMP-004**: Covered by `internal/ghostboard/auth_test.go` (digest, tokens, expiry, throttling),
  `internal/api/middleware/ghostboard_auth_test.go` (the gate on every token source),
  `internal/api/controller/ghostboard_pin_test.go` (unlock, lock, error envelopes),
  `internal/api/route/ghostboard_pin_route_test.go` (every board endpoint answers `401` without a
  session and works with one) and `internal/boardclient/pin_test.go` plus `cmd/board/pin_test.go`
  for the CLI.
- **IMP-005**: `ghostboard.pin_hmac`, `ghostboard.pin_secret`, `ghostboard.private_boards`,
  `ghostboard.session_ttl_secs`, `ghostboard.pin_max_attempts` and `ghostboard.pin_lockout_secs`
  are the knobs; the README documents each with its default and environment override.

## References

- **REF-001**: [ADR-0002 - Server-Sent Events for the ghostboard](0002-ghostboard-sse.md)
- **REF-002**: [ARCHITECTURE.md - Ghostboard Store](../ARCHITECTURE.md#ghostboard-store)
- **REF-003**: [README.md - Private boards](../../README.md#private-boards)
- **REF-004**: [docs/CLI.md - Private boards](../CLI.md#private-boards)
