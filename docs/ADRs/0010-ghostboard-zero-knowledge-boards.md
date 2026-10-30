---
title: "ADR-0010: Zero-knowledge ghostboards with the key in the fragment"
status: "Accepted"
date: "2026-10-30"
authors: "Maintainers"
tags: ["architecture", "ghostboard", "security", "crypto", "decision"]
supersedes: ""
superseded_by: ""
---

# ADR-0010: Zero-knowledge ghostboards with the key in the fragment

## Status

**Accepted**

## Context

A ghostboard is plain text on the server: `ghostboard.json` holds every board in clear, the store
serves it back, and the event stream pushes it to every open page (ADR-0002). The PIN gate
(ADR-0008) answers a different question — who may fetch a board — and says so explicitly in its
negative consequences: *"This is access control, not encryption"*.

The use that keeps coming back is one step past that: paste a password, a one-time invite link or a
short-lived note and keep it out of reach of the host itself. That is the case a homelab tool cannot
answer with a PIN, because the PIN and the text live on the same machine, and anyone who owns the
machine — or a backup of it, or the Docker socket, or the volume — reads everything.

The constraint is what the browser can honestly claim. GoSpin has no users and no key server, it is
served over plain HTTP on a LAN by default, and its pages are static files with Alpine and Tailwind
vendored under `ui/assets/vendor/` (no CDN in the shell anymore). Whatever design is chosen has to
survive that: the key must never reach the server, the crypto must run on the page, and the
"burned" state must be a fact the server enforces, not a checkbox the page remembers.

Two questions had to be answered: where the key lives, and what exactly "burn after read" means for
a server that cannot see the plaintext.

## Decision

A ghostboard can be **sealed**: the client encrypts before writing and decrypts after reading, and
the server stores one opaque envelope per board.

- **The key is 256 random bits, carried after the `#` in the URL** (`/ui/sealed/<board>#k=<key>`).
  The fragment is never sent in a request line, a header or a `Referer`, so the server, the reverse
  proxy and its access log never see it. The page reads `location.hash`, and `gb-cli` takes the key
  from `--key`, `--key-file` or `GO_SPIN_BOARD_KEY`.
- **AES-256-GCM through WebCrypto on the page, `crypto/aes` + `crypto/cipher` in `gb-cli`.** No
  key derivation: the key is already a random 256-bit value, so a KDF would only add a place to get
  it wrong. A fresh 96-bit IV per write, and the envelope carries `{"v":1,"iv":"…","ct":"…"}` in
  base64url. The board name is the additional authenticated data (`gospin-sealed-v1|<board>`), so an
  envelope copied into another board name fails to decrypt instead of decrypting into the wrong
  place.
- **The mode is a property of the board, set at creation and immutable.** The first write creates a
  sealed board (`X-Board-Mode: sealed`, or `mode: "sealed"` on the JSON surface); a later plaintext
  write to it is refused with `409`, so a buggy client cannot silently turn "the server cannot read
  this" into a plain board. A plain board is never converted in place: converting would have to read
  the plaintext, which is exactly the thing being ruled out.
- **Attachments stay out of sealed boards.** A board that carries a readable file is not
  zero-knowledge, and encrypting each upload changes the download, the ZIP archive, the resume
  support and the CLI in one step. The file routes answer `409` for a sealed board and the page
  hides the panel.
- **Burn-after-read is a server-side destroy on the first open, and the open is a `POST`.** A burn
  board carries `burn: true`. Reading it is `POST /ghostboard/:board/open` (mirrored on
  `/api/boards/:board/open`), which under the store mutex returns the envelope once, marks the board
  burned, persists that fact synchronously (the existing `Store.Flush`) and only then writes the
  body. A second open answers `410 Gone`, a restart answers `410 Gone`, and the name stays a
  tombstone for `ghostboard.sealed_burn_tombstone_secs` so a stale link cannot be pointed at a new
  note. `POST` rather than `GET` because a `GET` is fetched by link previews, browser prefetch and
  the search bar, any of which would burn the board before the reader sees it; a plain `GET` on an
  unopened burn board therefore answers `409` and reveals nothing. The page opens only after an
  explicit click, and fetches with `cache: "no-store"`. A burn board has no event stream either
  (`409`): the initial snapshot of `GET /ghostboard/:board/events` would consume the note before any
  reader asked for it.
- **The server owns the burn, not the client's success.** It cannot know whether the reader
  decrypted the envelope, so a truncated link or a wrong key still burns the note. The alternative —
  burning only after the client confirms — would require the server to verify a client secret, which
  is the PAKE problem this feature exists to avoid.
- **The page that holds the key loads nothing from another origin.** It is a dedicated
  `/ui/sealed/<board>` page built on the vendored Alpine and its own `ui/assets/sealed.js`, with a
  policy of its own (`default-src 'none'`, `script-src 'self' 'unsafe-eval'`, `style-src 'self'
  'unsafe-inline'`, `connect-src 'self'`) and no CDN origin at all. A third-party script on that
  page could read `location.hash` and the plaintext, which would make the whole feature theatre.
- **WebCrypto needs a secure context, and the page says so.** `crypto.subtle` exists only on HTTPS
  or `localhost`, so a sealed board opened on `http://192.168.1.10:8084` cannot decrypt. The page
  detects `window.isSecureContext`, refuses to pretend, and prints the equivalent `gb-cli` command;
  the CLI has no such constraint because the crypto runs in Go. Sealed boards therefore advertise
  HTTPS in the README rather than silently failing on the default LAN deployment.

## Consequences

### Positive

- **POS-001**: The server, its data file, its backups and anyone holding the Docker socket see
  ciphertext only, which is the property a PIN cannot offer.
- **POS-002**: No new state and no key server: the key is in the link the creator already has to
  send, and a restart neither loses it nor weakens it.
- **POS-003**: The burn is enforced where the data lives, so a second reader, a reload, a cached
  response or a restart cannot resurrect the note.
- **POS-004**: The feature reuses the existing surfaces — the same board endpoints, the same event
  stream for a sealed board that is not a burn board, the same limits, the same persistence flush —
  instead of a parallel stack.

### Negative

- **NEG-001**: The URL is the secret, and URLs leak: browser history, the address bar, a clipboard,
  a screenshot, browser sync, a chat preview that keeps the link. The fragment keeps it out of
  server logs but not out of those places, and the documentation has to say so.
- **NEG-002**: Burn-after-read can be triggered by anyone who holds the link without the fragment,
  so a board can be destroyed without being read. On a single-user homelab this is the same person;
  the risk is accepted and stated.
- **NEG-003**: The server sees metadata it cannot hide: the board name (which is in the path), the
  envelope size, the write and open times and the client address. Sealed means the content, not the
  fact that a board exists and was read.
- **NEG-004**: A malicious server can serve stale or substituted ciphertext, or simply withhold it;
  there is no transparency log and no client-side consistency proof. The guarantee is
  confidentiality, not availability or freshness.
- **NEG-005**: Sealed boards cannot carry attachments, cannot be converted from an existing plain
  board, and cannot be opened in a browser served over plain HTTP.
- **NEG-006**: `max_text_bytes` now bounds the envelope, not the plaintext: the ciphertext plus the
  base64 wrapping costs about a third more, so the page and the CLI cap the typed text below the
  configured value and show the effective limit.

## Alternatives Considered

### PIN-protected boards only (ADR-0008)

- **ALT-001**: **Description**: Point at the existing gate and document that a private board is
  enough.
- **ALT-002**: **Rejection Reason**: The PIN protects the route, not the file: whoever reads
  `ghostboard.json`, a backup or a volume snapshot has the text. The feature was asked for precisely
  because that is not enough.

### Encryption at rest with a key from the environment

- **ALT-001**: **Description**: Encrypt the whole `ghostboard.json` with `GO_SPIN_GHOSTBOARD_DATA_KEY`
  and keep the board plaintext in memory.
- **ALT-002**: **Rejection Reason**: It only defends the file at rest, and it keeps every key on the
  same host as the ciphertext, so a compromised server reads everything. It is a reasonable
  separate hardening step, not this feature.

### The key in the query string or a custom header

- **ALT-001**: **Description**: `/ui/sealed/<board>?k=<key>`, or `X-Board-Key` on the API.
- **ALT-002**: **Rejection Reason**: The query string lands in access logs, proxy logs, `Referer`
  headers and browser history on the server side, which defeats the point; a header works for the
  CLI but the browser page has to get the key from somewhere, and the address bar is all it has.

### A server-generated key, wrapped for the client

- **ALT-001**: **Description**: The server creates the key, encrypts the board with it and hands it
  to the creator once.
- **ALT-002**: **Rejection Reason**: The server holds the key at least once and can keep a copy,
  which makes the claim unverifiable. "Zero-knowledge" has to mean the key never arrives.

### Burn on `GET`, with no confirmation step

- **ALT-001**: **Description**: `GET /ghostboard/:board` returns the envelope and destroys it.
- **ALT-002**: **Rejection Reason**: A link preview, a browser prefetch or a service-worker
  navigation burns the note before the reader opens it. A `POST` cannot be prefetched.

### Burn after the client confirms decryption

- **ALT-001**: **Description**: The server hands out the envelope, the page decrypts, then calls
  `DELETE` to burn.
- **ALT-002**: **Rejection Reason**: Between the fetch and the delete the note is readable by anyone
  holding the link, and a closed tab leaves it unburned, so the guarantee becomes best-effort. It
  also invites a "prove you decrypted it" protocol the server cannot check without the key.

### A passphrase with a KDF instead of a random key in the fragment

- **ALT-001**: **Description**: The fragment carries no key; the reader types a passphrase, PBKDF2
  (or Argon2 in a vendored library) derives the key.
- **ALT-002**: **Rejection Reason**: It trades 256 bits of entropy for what a human remembers, adds
  a KDF implementation to audit in JavaScript, and buys nothing for a link that is already shared
  out of band. It can be added later on top of the same envelope, as a second way to obtain the key.

### Key handling on the existing ghostboard page

- **ALT-001**: **Description**: The requested toggle: mint the key, prepare the link and create the
  board inline on `/ui/ghostboard`, next to the plain board.
- **ALT-002**: **Rejection Reason**: That page keeps the CDN origins in its policy for
  deployments that still load the libraries from a remote origin, and no page whose policy allows
  third-party scripts may ever hold the key or the plaintext: one injected script reads
  `location.hash` and the claim collapses. Creation therefore stays a dedicated flow at
  `/ui/sealed`, reached by a navigation-only link from the ghostboard page and the dashboard —
  only navigated to, never mixed in — and the key lives only inside the no-CDN page.

### A vendored JavaScript AES implementation instead of WebCrypto

- **ALT-001**: **Description**: Ship a small audited library under `ui/assets/vendor/` so sealed
  boards also work over plain HTTP, where `crypto.subtle` is undefined.
- **ALT-002**: **Rejection Reason**: It removes the secure-context restriction at the price of
  hand-auditing crypto in the repository and a cache-busting asset per update, while the CLI already
  covers the plain-HTTP case with the platform's implementation. WebCrypto is the smaller attack
  surface.

## Implementation Notes

- **IMP-001**: `Board` (`internal/ghostboard/store.go`) gains `Mode` (`plain` | `sealed`) and
  `Burn bool`; the text field carries the envelope for a sealed board. Missing fields in an existing
  `ghostboard.json` read as `plain`, so no migration runs.
- **IMP-002**: `Store.Update` keeps its signature for plain boards and gains the sealed path
  (`Store.UpdateSealed`, `Store.Open`) that validates the envelope shape and refuses a plaintext
  write to a sealed board. `Store.Open` marks the tombstone and calls `Flush` before returning, so
  the burn survives a crash between the response and the next persistence tick.
- **IMP-003**: The crypto lives in `internal/sealedcrypto` (Go, for the CLI) and in
  `ui/assets/sealed.js` (WebCrypto, for the page). Both are pinned by one known-answer vector so the
  two implementations cannot drift; the envelope version is the one place to change if a cipher ever
  has to be replaced.
- **IMP-004**: New configuration keys: `ghostboard.sealed_enabled` (default `true`, turning the
  routes into `404` when false) and `ghostboard.sealed_burn_tombstone_secs` (default `86400`, how
  long a burned name answers `410`). `max_text_bytes` continues to bound what is stored, now the
  envelope.
- **IMP-005**: The routes are `POST /ghostboard/:board/open` and `POST /api/boards/:board/open`,
  registered next to the existing board routes and behind `GhostboardPIN` like every other board
  endpoint, so a sealed board named in `ghostboard.private_boards` still needs the PIN first.
- **IMP-006**: `ui/sealed.html` and `ui/assets/sealed.js` are served by `NewUIRouter` with their own
  Content-Security-Policy (`middleware.SealedPageContentSecurityPolicy`) and no CDN origin. The page
  stays out of the service-worker precache and fetches the board with `cache: "no-store"`, so a
  burned board cannot be replayed from a cache: the page itself carries no secret, the envelope
  does.
- **IMP-007**: `gb-cli` gains `sealed new`, `open` and `--key`/`--key-file`/`GO_SPIN_BOARD_KEY` on
  `read`/`write`/`set`/`add`; `follow` refuses a burn board (there is no stream to consume) and
  decrypts every snapshot of a live sealed one.
- **IMP-008**: Covered by store tests (create, plaintext refusal, exactly-one open under
  concurrency, tombstone across a store reload, burn flush failure), crypto tests (both
  implementations against the shared vector), controller and route tests for the status codes, CLI
  tests for key resolution and the missing-key exit code, and a Playwright check that the page
  decrypts and that it loads no third-party script.

## References

- **REF-001**: [ADR-0002 - Server-Sent Events for the ghostboard](0002-ghostboard-sse.md)
- **REF-002**: [ADR-0008 - PIN protection for private ghostboards](0008-ghostboard-pin-protection.md)
- **REF-003**: [ADR-0009 - Board-scoped file attachments](0009-ghostboard-file-attachments.md)
- **REF-004**: [ARCHITECTURE.md - Ghostboard Store](../ARCHITECTURE.md#ghostboard-store)
- **REF-005**: [README.md - Ghostboard](../../README.md#ghostboard) and
  [README.md - Security Headers](../../README.md#security-headers), where `Referrer-Policy:
  no-referrer` keeps the fragment out of an outgoing `Referer`.
- **REF-006**: [docs/CLI.md - gb-cli reference](../CLI.md)
