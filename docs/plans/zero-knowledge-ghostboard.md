# Zero-knowledge ghostboards: encrypted boards with a fragment key and burn-after-read

## 1. Title

Sealed ghostboards: AES-256-GCM in the browser with the key in the fragment, optional burn-after-read.

## 2. Context and Goal

Ghostboards are plain text on the server. ADR-0008 added a PIN gate, which is access control and
says so; ADR-0010 decides to add a mode where the server stores ciphertext it cannot read, plus an
optional single-read board whose content the server destroys on the first open.

This plan turns that decision into code. "Done" means a user can create a sealed board from the page
or from `gb-cli`, write and read it with the key in the link, mark it burn-after-read so it is gone
after one open, and nothing on the server — the JSON file, the API, the logs — ever contains the
plaintext or the key. The reasoning, the threat model and the rejected alternatives live in
[ADR-0010](../ADRs/0010-ghostboard-zero-knowledge-boards.md); this document only sequences the work.

## 3. Status

- **State**: `done`
- **Last updated**: `2026-10-30`
- **Blockers**: none.

## 4. Scope

### In

- `internal/sealedcrypto`: envelope format, AES-256-GCM, key parsing, one shared known-answer vector.
- `internal/ghostboard`: board mode and burn flag, sealed write path, `Open`/burn with a synchronous
  flush, tombstone, sealed-board refusals for attachments and for plaintext writes.
- `internal/config`: `ghostboard.sealed_enabled` and `ghostboard.sealed_burn_tombstone_secs`, with
  defaults, validation and tests.
- `internal/api/route` and `internal/api/controller`: the sealed reads and the
  `POST /ghostboard/:board/open` (plus `/api/boards/:board/open`) route, stable status codes and
  tests on both surfaces.
- `internal/api/middleware`: `SealedPageContentSecurityPolicy` (no CDN origin) and its test.
- `ui/sealed.html`, `ui/assets/sealed.js`, the create/reveal flow, the insecure-context message, and
  the `NewUIRouter` wiring that serves them with their own policy.
- `cmd/board` and `internal/boardclient`: `sealed new`, `open`, key resolution through `--key`,
  `--key-file` and `GO_SPIN_BOARD_KEY`, decryption on `read`/`follow`.
- Documentation: README (API table, configuration, a "Sealed boards" section carrying the threat
  model), `docs/CLI.md`, `docs/ARCHITECTURE.md`, `docs/FRONTEND.md`, `CHANGELOG.md`,
  `docs/README.md` (index row for the ADR and this plan).

### Out

- Encrypting attachments, or a sealed board that carries files at all: the file routes refuse a
  sealed board.
- Converting an existing plain board, or re-keying one: a sealed board is created sealed, and a lost
  key means a lost board.
- A passphrase-to-key KDF, QR-code sharing, key escrow, per-user keys, expiry shorter than the
  existing idle clear.
- Client-side proof of decryption: the burn is server-side on the open request (ADR-0010).
- PWA offline support for the sealed page: it is not registered with the service worker.
- Changes to ADR-0008 semantics: the PIN gate keeps working as it does today, in front of sealed
  boards too.

## 5. Task List

| ID    | Task                                                                                                                                                                  | Depends on     | Complexity | Done |
| ----- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------- | ---------- | ---- |
| T-001 | `internal/sealedcrypto`: envelope `{v, iv, ct}`, AES-256-GCM with `gospin-sealed-v1\|<board>` as AAD, `NewKey`, `ParseKey`, `Seal`, `Open`, size accounting; unit tests | –              | M          | yes  |
| T-002 | One known-answer vector (fixed key, IV, board and plaintext) consumed by a Go test and by the page implementation, so the two encoders cannot drift                     | T-001          | S          | yes  |
| T-003 | `ui/assets/sealed.js`: WebCrypto `Seal`/`Open`, base64url helpers, `isSecureContext` detection, no third-party import                                              | T-002          | M          | yes  |
| T-004 | `internal/ghostboard`: `Board.Mode` and `Board.Burn`, sealed update path, plaintext-write refusal, sealed-board refusal for uploads/archives                          | –              | M          | yes  |
| T-005 | `Store.Open`: atomically read-once, mark burned, `Flush` before the envelope leaves the store, tombstone for `sealed_burn_tombstone_secs`, `410` on every later read   | T-004          | M          | yes  |
| T-006 | Store tests: create as sealed, plaintext write rejected, exactly one successful open under concurrent callers, burn survives a store reload, flush failure fail-closed | T-005          | M          | yes  |
| T-007 | `internal/config`: `ghostboard.sealed_enabled` (default `true`) and `ghostboard.sealed_burn_tombstone_secs` (default `86400`), validation, `config/config.yaml`, README table and config tests | –       | S          | yes  |
| T-008 | Controller: sealed reads return the envelope in `text`, `open` handler, error mapping (`400` malformed envelope, `409` plaintext write to a sealed board, `409` files on a sealed board, `409` plain `GET` or event stream on an unopened burn board, `410` burned, `404` sealed disabled) | T-005, T-007   | M          | yes  |
| T-009 | Routes: `POST /ghostboard/:board/open` and `POST /api/boards/:board/open` behind `GhostboardPIN`, route tests for every status code and for the PIN interaction      | T-008          | S          | yes  |
| T-010 | `middleware.SealedPageContentSecurityPolicy` (no CDN origin, `connect-src 'self'`) plus its test and the header assertions on `/ui/sealed/<board>`                     | –              | S          | yes  |
| T-011 | `ui/sealed.html` and its Alpine component: create flow (mint key, show link), reveal flow (explicit button, `cache: "no-store"`, `POST` open), insecure-context message with the equivalent `gb-cli` command | T-003, T-009, T-010 | L   | yes  |
| T-012 | `NewUIRouter`: serve the page and its assets with the sealed policy, no service-worker registration, no-store on the board JSON                                           | T-010, T-011   | S          | yes  |
| T-013 | `internal/boardclient` + `cmd/board`: `sealed new` (prints the link and the key, `--json`), `open`, `--key`/`--key-file`/`GO_SPIN_BOARD_KEY`, decryption on `read` and `follow`, refusals for burn boards and files, exit codes documented | T-003, T-009   | L          | yes  |
| T-014 | CLI tests: key resolution precedence, missing key, wrong key, burned board, `open` on a non-burn board, `--json` report                                                       | T-013          | M          | yes  |
| T-015 | Documentation: README "Sealed boards" (threat model, HTTPS requirement, lost-key warning), API rows, `docs/CLI.md`, `docs/ARCHITECTURE.md`, `docs/FRONTEND.md`, CHANGELOG, `docs/README.md` index | T-008…T-013    | M          | yes  |
| T-016 | End-to-end pass on a running instance: page create/reveal, `gb-cli` round trip, ciphertext-only data file, burn observed by a second client and after a restart, Playwright check for third-party requests | T-012, T-014, T-015 | M    | yes  |

Complexity: `XS`, `S`, `M`, `L`, `XL`. No time estimates.

## 6. Verification

The plan closes when all of this passes:

- `task check` (fmt, vet, staticcheck, golangci-lint, tests) and `task build`; `task test:race` for
  the store and the burn path, unless the host is a Raspberry Pi 4 or another kernel that refuses
  the ThreadSanitizer VMA layout, in which case the limitation is stated in the summary instead of
  being counted as a pass.
- The new tests, named explicitly: `internal/sealedcrypto/*_test.go` (round trip, tampering, wrong
  board name in the AAD, wrong key, vector), `internal/ghostboard/sealed_test.go` (mode, refusals,
  concurrent open, reload, flush failure), `internal/api/controller/ghostboard_sealed_test.go`,
  `internal/api/route/ghostboard_sealed_route_test.go`, `internal/api/middleware/security_headers_test.go`
  (the sealed policy), `internal/boardclient/sealed_test.go`, `cmd/board/sealed_test.go`.
- Behaviour exercised the way a user does it: `gb-cli sealed new --burn`, `gb-cli write`, `gb-cli
  open`, a second `open` answering `410`, and the page in a browser (Playwright) showing the
  decrypted text once, never loading a third-party script, and refusing on a plain-HTTP origin that
  is not `localhost`.
- The data file inspected after a sealed write: `grep` for the plaintext finds nothing, and the
  envelope is the only thing stored.
- Coverage measured with `go test -cover ./...`; every touched package at or above 70%, with the
  crypto and burn paths at 100% of their new branches. A package below the target is named in the
  summary with the reason, never papered over.
- Documentation updated in the same change, including the README API table, the configuration table
  and the CHANGELOG entry under `## [Unreleased]`.

### Verification record

`2026-10-30`, feature complete.

- `task check` and `task build` pass; `go test ./...` is green.
- Coverage of the touched packages: `internal/sealedcrypto` 86.8%, `internal/ghostboard` 87.8%,
  `internal/api/controller` 80.5%, `internal/api/route` 88.7%, `internal/api/middleware` 80.2%,
  `internal/config` 94.9%, `internal/boardclient` 84.8%, `cmd/board` 80.8%.
- `task test:race` could not run on this host: the kernel refuses the ThreadSanitizer VMA layout
  (`FATAL: ThreadSanitizer: unsupported VMA range`). The concurrency of the burn path is covered
  instead by `TestOpenSucceedsForExactlyOneConcurrentCaller` under the normal detector.
- `gb-cli sealed new` → `read --key` → `sealed new --burn` → `open` → second `open` answering `410`
  was exercised against a real server on the memory runtime; after a restart the sealed board still
  read and the burn still answered `410` in `ghostboard.json`.
- The data file after a sealed write carries the envelope and a tombstone and no plaintext.
- `ui/assets/sealed.js` was run under Node 22 with WebCrypto: `window.sealedSelfTest()` reproduces
  the published vector byte for byte, the page component reads the key from the fragment and
  refuses a plain-HTTP origin. A Chromium run was not possible on this host; the CSP, the absence of
  third-party references and the absence of the service worker are asserted by
  `TestSealedPageCarriesItsOwnPolicy`.

## 7. Risks and Decisions

- **Lost key, lost note.** The key exists only in the link. The create flow must say so before the
  board is written, and the CLI prints the link as the only copy. No recovery path is added.
- **Ciphertext growth.** `max_text_bytes` bounds the envelope, which is roughly a third larger than
  the plaintext after base64. The page and the CLI compute the effective plaintext limit from it and
  show it, instead of letting the server answer `413` after the user typed a long note.
- **Plain HTTP.** WebCrypto is unavailable outside a secure context, so the page refuses and points
  at `gb-cli`. The README says that sealed boards need HTTPS; the alternative (vendoring a JS AES
  implementation) is recorded and rejected in ADR-0010.
- **Fragment leakage.** History, clipboard, screenshots and chat previews keep the key long after
  the page is closed. The README documents it; the page offers a "copy link" button and nothing
  that stores the key (`localStorage`, `sessionStorage` and cookies stay untouched).
- **Burn griefing.** Anyone holding the link can burn the board without reading it; accepted for a
  single-user tool and stated in the README. A wrong key or a failed decrypt burns it too, which is
  why the reveal is a deliberate click and not an automatic fetch.
- **Prefetch and caching.** A `GET` open would be burned by link previews and browser prefetch, and
  a cached envelope would let a burned board be read twice: hence `POST`, `no-store` and no service
  worker for the sealed page.
- **Two crypto implementations.** The Go CLI and the browser must produce the same envelope. The
  shared known-answer vector is the guard; a change to the envelope version touches both or the
  test fails.
- **A decision that may deserve its own ADR.** If encrypted attachments or a passphrase KDF is
  wanted later, both change the envelope format and the threat model: write a new ADR instead of
  extending ADR-0010 quietly.

## 8. References

- [ADR-0010 - Zero-knowledge ghostboards with the key in the fragment](../ADRs/0010-ghostboard-zero-knowledge-boards.md)
- [ADR-0002 - Server-Sent Events for the ghostboard](../ADRs/0002-ghostboard-sse.md)
- [ADR-0008 - PIN protection for private ghostboards](../ADRs/0008-ghostboard-pin-protection.md)
- [ADR-0009 - Board-scoped file attachments](../ADRs/0009-ghostboard-file-attachments.md)
- [ARCHITECTURE.md - Ghostboard Store](../ARCHITECTURE.md#ghostboard-store)
- [FRONTEND.md](../FRONTEND.md), [CLI.md](../CLI.md), [README.md - Ghostboard](../../README.md#ghostboard)
- [board-uploads-spec.md](board-uploads-spec.md): the shape an implemented board feature takes in this
  repository (configuration, HTTP surface, lifecycle, UI, CLI, acceptance criteria).
