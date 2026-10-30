# Sealed ghostboards - first implementation pass (crypto and store)

**Status**: implemented with the whole plan (T-001 ... T-016), 2026-10-30
**Last updated**: 2026-10-30
**Short name**: `sealed-ghostboards`
**Extends**: [zero-knowledge-ghostboard.md](zero-knowledge-ghostboard.md) (T-001 ... T-006, plus the
configuration keys of T-007)

## 1. Context and goal

[ADR-0010](../ADRs/0010-ghostboard-zero-knowledge-boards.md) is Accepted and
[the plan](zero-knowledge-ghostboard.md) is `in-progress` with all sixteen tasks still open: a
ghostboard can be **sealed**, so the client encrypts before writing and decrypts after reading, and
the server holds one opaque envelope per board, optionally destroying it on the first open
(burn-after-read).

This pass implements the part everything else depends on and that can be verified without a
browser, an HTTP route or a CLI:

- `internal/sealedcrypto`: the envelope, AES-256-GCM, key parsing, size accounting and the shared
  known-answer vector (T-001, T-002);
- `internal/ghostboard`: `Board.Mode`, `Board.Burn`, the sealed write path, `Store.Open` with its
  synchronous flush, the tombstone, and the refusals a sealed board needs (T-003 is the page, so
  it waits; T-004, T-005, T-006);
- `internal/config`: `ghostboard.sealed_enabled` and `ghostboard.sealed_burn_tombstone_secs`,
  pulled forward from T-007 because the store needs the duration and the flag.

"Done" means: `go test ./...` proves that a board can be created sealed, that a plaintext write to
it is refused, that exactly one concurrent caller can open a burn board, that the burn survives a
reload of the store and a failed flush never hands out the envelope, and that the Go implementation
round-trips the shared vector the future page will use. Nothing is exposed on HTTP yet, so no
user-visible behaviour changes and the README keeps describing today's product.

## 2. Decisions taken in the interview

| Topic | Decision |
| ----- | -------- |
| Scope of this pass | T-001 ... T-006 in full, plus the two configuration keys of T-007. Routes, controller, CSP middleware, UI, CLI and the end-to-end pass are later. |
| Version control | Work goes straight on `develop`, as requested; no feature branch. |
| Sealed board names | Chosen by the user, in the same namespace as plain boards. A plain board that already holds text can never become sealed. |
| Creation entry point | Wanted as a **toggle on the existing ghostboard page**. Delivered as a dedicated create flow at `/ui/sealed` instead, because ADR-0010 requires that the page holding the key load nothing from another origin and the ghostboard page keeps the CDN origins in its policy for the vendored fallbacks: putting the key there would contradict the decision the feature rests on. Creation is still one click away from both surfaces: `ui/ghostboard.html` and `ui/home.html` carry a `<!-- SEALED_BOARD_ENTRY -->` marker that the route replaces with a navigation-only link to `/ui/sealed` when `ghostboard.sealed_enabled` is on. The key is minted, used and forgotten entirely inside the sealed page; the docs and `gb-cli sealed new` carry the direct URL. |
| Config source | The two keys are added to viper now (defaults, validation, `config/config.yaml`), not deferred to T-007. |
| Known-answer vector | A JSON file **served to the UI** (`ui/assets/sealed-vector.json`), read by the Go test and later loadable by the page. |
| Flush failure during `Open` | **No revelation, retryable**: the error goes up, the envelope stays in the store, a later attempt may succeed. |
| Tombstone expiry | After `sealed_burn_tombstone_secs` the tombstone is dropped and the name is free again; a stale link answers `404`, never the content. |
| Unopened burn board and the idle clear | The burn board gets the **longer** `sealed_burn_tombstone_secs` inactivity window instead of `clear_delay`, so a note is not silently emptied before it is read. |
| Plain board with empty text | **May become sealed**: there is no plaintext to protect, so the first sealed write converts it. |
| `Store.Open` scope | Only a burn board can be opened. A plain board and a sealed non-burn board are refused with a dedicated error and read with `Get`. |
| Error representation | Exported sentinel errors in `internal/ghostboard`, matched with `errors.Is`, like `ErrTextTooLarge` today. |
| Tombstone persistence | A separate `tombstones` array in `ghostboard.json`, next to `boards`. |
| `sealed_enabled` semantics | With the key `false` the store **refuses sealed writes**; reads and opens keep working so disabling never strands ciphertext. |
| Key text format | `base64url` without padding when emitting; parsing also accepts lowercase **hex** and a **full link** with `#k=...`. |
| Sealed write API | `Store.UpdateSealed(SealedWrite)`, a struct rather than a long argument list. |
| Spec language | English, like every document in the repository. |
| Documentation in this pass | Only the plan's task table and date. README, CHANGELOG and ARCHITECTURE follow with the routes (T-015). |

## 3. Scope

### In

- `internal/sealedcrypto/*.go` and its tests, including the vector test.
- `ui/assets/sealed-vector.json`: the vector file itself (no page, no script yet).
- `internal/ghostboard`: `Mode`, `Burn`, `SealedWrite`, `UpdateSealed`, `Open`, tombstones,
  refusals, persistence and idle-clear changes, with tests.
- `internal/config`: the two keys, defaults, validation, `config/config.yaml` and `config_test.go`.
- `docs/plans/zero-knowledge-ghostboard.md`: `Done` for T-001 ... T-006, a note on the partial
  T-007, and a new "Last updated" date.

### Out

- Everything HTTP: routes, controller, status codes, `middleware.SealedPageContentSecurityPolicy`
  (T-008, T-009, T-010, T-012).
- `ui/sealed.html`, `ui/assets/sealed.js` and the ghostboard page toggle (T-003, T-011), including
  the WebCrypto implementation of the vector.
- `gb-cli` and `internal/boardclient` (T-013, T-014).
- README, CHANGELOG, ARCHITECTURE and FRONTEND updates, and the end-to-end pass (T-015, T-016).
- Any change to ADR-0008 (the PIN gate) or to ADR-0009 (attachments). A sealed board simply
  refuses files.
- Encrypting attachments, converting a sealed board back to plain, re-keying, a passphrase KDF,
  key escrow and PWA support for the sealed page: all rejected in ADR-0010.

## 4. Configuration

Two keys are added now, both documented in `config/config.yaml` with a comment:

| Key | Environment | Default | Meaning |
| --- | ----------- | ------- | ------- |
| `ghostboard.sealed_enabled` | `GO_SPIN_GHOSTBOARD_SEALED_ENABLED` | `true` | Allows sealed writes. `false` makes the store refuse them; reads and opens keep working. |
| `ghostboard.sealed_burn_tombstone_secs` | `GO_SPIN_GHOSTBOARD_SEALED_BURN_TOMBSTONE_SECS` | `86400` | How long a burned name answers as burned, and the inactivity window of an unopened burn board. |

- `config.GhostboardConfig` gains `SealedEnabled bool` and `SealedBurnTombstone time.Duration`;
  `WithDefaults()` and `DefaultGhostboardConfig()` fill them, so a zero-valued config built in a
  test still behaves like the documented defaults.
- Validation fails fast: `sealed_burn_tombstone_secs` must be positive.
- `ghostboard.Config` (the store's own struct) gains `SealedEnabled bool` and
  `SealedTombstone time.Duration`; `ghostboard.New` applies the same defaults when they are zero,
  so no caller can create a store with a zero-length tombstone.
- `internal/app` passes both values through when constructing the store.

## 5. The envelope (`internal/sealedcrypto`)

### 5.1 Format

```json
{"v":1,"iv":"<16 base64url chars>","ct":"<base64url of ciphertext+tag>"}
```

- AES-256-GCM, 96-bit random IV per encryption, 128-bit tag appended to the ciphertext by Go's
  `cipher.AEAD`.
- Additional authenticated data: `gospin-sealed-v1|<board>`, where `<board>` is the exact board
  name. Copying an envelope to another board name therefore fails to decrypt.
- `v` is `1`. Any other version is refused with an explicit error, so a future cipher version is a
  new value and never a silent reinterpretation.
- Base64 is `base64.RawURLEncoding` (no padding) in every field.
- The board name is never inside the envelope: it is the AAD and the path.

### 5.2 API

```go
const (
    KeySize   = 32
    IVSize    = 12
    Version   = 1
)

type Key [KeySize]byte

func NewKey() (Key, error)                      // crypto/rand
func ParseKey(text string) (Key, error)         // base64url, hex, or a link with #k=
func (k Key) String() string                    // base64url, 43 chars

func Seal(key Key, board string, plaintext []byte) (string, error)
func Open(key Key, board, envelope string) ([]byte, error)
func ParseEnvelope(envelope string) (Envelope, error)   // shape and version only

func EnvelopeLen(plaintextLen int) int          // exact size of the JSON envelope
func MaxPlaintextBytes(envelopeLimit int) int   // inverse of EnvelopeLen
```

- `Seal` returns the JSON envelope as a string; `Open` returns the plaintext.
- Errors are exported sentinels: `ErrInvalidKey`, `ErrInvalidEnvelope`, `ErrUnsupportedVersion`,
  `ErrDecrypt` (wrong key, wrong board, tampered ciphertext — deliberately one error, so the caller
  cannot tell a wrong key from a corrupted envelope; the message names no secret).
- `ParseKey` trims surrounding whitespace, accepts `#k=<key>` and `#<key>` inside a full URL, and
  rejects anything that is not exactly 32 bytes after decoding. It never logs the value it rejects.
- `ParseEnvelope` is what `internal/ghostboard` calls, so the store and the CLI validate the shape
  in one place instead of duplicating a regex.

### 5.3 Size accounting

The envelope grows non-linearly because of base64. `EnvelopeLen(n)` is exact:

```text
envelopeLen(n) = 39 + ceil(4 * (n + 16) / 3)
```

(13 bytes of `{"v":1,"iv":"`, 16 characters of IV, 8 bytes of `","ct":"`, the base64 body, 2
bytes of `"}`.) `MaxPlaintextBytes(limit)` returns the largest `n` with
`EnvelopeLen(n) <= limit`, and a table test asserts the two functions are inverses around the
boundary. `max_text_bytes` keeps bounding the envelope, so the page and the CLI will use
`MaxPlaintextBytes` to show the real limit instead of letting the server answer `413` after the
user typed a long note.

### 5.4 The shared vector

`ui/assets/sealed-vector.json` is the single reference both implementations must reproduce:

```json
{
  "version": 1,
  "board": "vector-board",
  "key": "<43 base64url chars>",
  "iv": "<16 base64url chars>",
  "plaintext": "the quick brown fox jumps over the lazy dog",
  "envelope": "{\"v\":1,\"iv\":\"...\",\"ct\":\"...\"}"
}
```

- The Go test reads it from `../../ui/assets/sealed-vector.json` and asserts that sealing
  `plaintext` for `board` with the fixed `iv` produces `envelope` byte for byte, and that opening
  it returns `plaintext`.
- Deterministic sealing uses an unexported `sealWithIV(key, board, plaintext, iv)`; the in-package
  test calls it directly, so no IV-injection hook is exported for production code.
- The file is served by the existing `/ui/assets` static route with a one-year cache. It must only
  ever contain this **published test key**: a real key in that file would be a leaked key. That
  sentence belongs in the file's neighbourhood (a `README` note or a comment in the JSON via a
  field is not possible, so the plan documents it and the test refuses a vector whose key equals a
  key generated at runtime — a cheap guard against a slip).

## 6. Data model and store (`internal/ghostboard`)

### 6.1 `Board`

```go
type Mode string

const (
    ModePlain  Mode = "plain"
    ModeSealed Mode = "sealed"
)

type Board struct {
    UpdatedAt time.Time `json:"updated_at"`
    Name      string    `json:"name"`
    Text      string    `json:"text"`   // the envelope for a sealed board
    Mode      Mode      `json:"mode,omitempty"`
    Files     []File    `json:"files"`
    Burn      bool      `json:"burn,omitempty"`
    Revision  uint64    `json:"revision"`
}
```

- `omitempty` keeps every existing plain board's JSON identical on disk and on the API, so an old
  file loads unchanged and `ModePlain` is the zero-value meaning of a missing field: **no
  migration**.
- The text of a sealed board is the envelope string. The store never decrypts, never inspects and
  never logs it.

### 6.2 `ghostboard.json`

```json
{
  "boards": [
    {
      "name": "nota",
      "text": "{\"v\":1,\"iv\":\"...\",\"ct\":\"...\"}",
      "mode": "sealed",
      "burn": true,
      "revision": 3,
      "updated_at": "2026-10-30T12:00:00Z",
      "files": []
    }
  ],
  "tombstones": [
    { "name": "nota-bruciata", "burned_at": "2026-10-30T11:00:00Z" }
  ]
}
```

- `fileDocument` gains `Tombstones []tombstone` with `json:"tombstones,omitempty"`; `tombstone` is
  `{name, burned_at}`.
- The tombstone is a sibling of the boards, not a board with an empty text: a burned board has no
  envelope left, and `persistedBoardsLocked` already skips empty boards, so a board-shaped
  tombstone would need a special case in three places.
- `load` validates tombstone names with `ValidateBoardName`, drops tombstones older than the
  configured window (they answer `404` from then on), and rebuilds the in-memory map.
- `Flush` writes the live tombstones; a tombstone is not "dirty" state of its own, the store
  simply sets `dirty` when one is added or expires.

### 6.3 Store API

```go
type SealedWrite struct {
    Name     string
    Envelope string
    ClientID string
    Burn     bool
}

func (s *Store) UpdateSealed(w SealedWrite) (Board, error)
func (s *Store) Open(name string) (Board, error)
```

**`UpdateSealed`**

1. Validate the board name and the envelope (`sealedcrypto.ParseEnvelope`).
2. Refuse when `sealed_enabled` is off (`ErrSealedDisabled`).
3. Refuse an envelope larger than `max_text_bytes` (`ErrTextTooLarge`), before touching state.
4. Look up the board under the mutex:
   - sealed and not burned → rewrite the envelope, `Burn` is fixed at creation and a different
     value is ignored with a warning log;
   - plain with text → `ErrSealedOverPlainText` (the `409` of T-008);
   - plain and empty, or unknown → create the board with `Mode: ModeSealed`, `Burn: w.Burn`;
   - a tombstoned name inside the window → `ErrBoardBurned`; after the window the tombstone is
     already gone, so the name is free.
5. An envelope identical to the current one is a no-op, exactly like `Update` with the same text:
   no revision bump, no event.
6. Otherwise bump the revision, set `UpdatedAt`, broadcast the event with the envelope (a sealed
   board that is not a burn board keeps its live stream; the client decrypts each snapshot) and
   arm the idle clear.

**`Open`**

1. Validate the name.
2. Refuse when `sealed_enabled` is off (`ErrSealedDisabled`).
3. Under the mutex:
   - tombstone inside the window → `ErrBoardBurned`;
   - unknown board, or expired tombstone → `ErrBoardNotFound`;
   - plain board, or sealed board with `Burn == false` → `ErrBoardNotSealed`;
   - burn board whose envelope is empty → `ErrBoardNotFound` (nothing to destroy, nothing to burn);
   - otherwise: take the envelope, remove the board record, insert the tombstone with
     `burned_at = now`, set `dirty`.
4. Call `Flush` **before** returning, so the burn is on disk before the body leaves the store. If
   the flush fails, roll back in memory (restore the board, drop the tombstone) and return the
   wrapped error: the caller learns nothing, and a later attempt can succeed. That is the "no
   revelation, retryable" decision.
5. Return a `Board` carrying the envelope, `Mode: ModeSealed`, `Burn: true` and the revision
   incremented by one.

**Refusals carried over from the sealed model**

- `Update` (plaintext write) on a sealed board, burn or not → `ErrPlaintextWriteToSealed`.
- Any attachment operation on a sealed board — add, list, delete, archive — →
  `ErrSealedBoardFiles`. The file routes will map it to `409` in T-008; the store is the layer that
  refuses.

### 6.4 Read path

- `Get` on a sealed board returns the envelope in `Text` and `Mode: ModeSealed`.
- `Get` on a burn board that has not been opened returns `ErrBurnRequiresOpen`, so a plain `GET`
  cannot consume or reveal the note (the `409` of the ADR).
- `Get` on a tombstoned name returns `ErrBoardBurned`; after the window the name is unknown again
  and `Get` returns the usual empty board.
- `Subscribe` on a burn board returns `ErrBurnRequiresOpen`: the initial snapshot of the event
  stream would consume the note before a reader asked for it.
- `Subscribe` on a live sealed board works and carries envelopes.

### 6.5 Idle clear

`scheduleIdleClearLocked` picks the delay per board: `sealed_burn_tombstone_secs` for a burn board,
`clear_delay` for everything else. When the timer fires on an unopened burn board, the envelope is
destroyed, a tombstone is recorded and `dirty` is set: the note is gone, a stale link answers
`410` for the tombstone window and `404` afterwards. Cleared boards keep their `Mode`, so a sealed
board that goes idle stays sealed and its next write must still be an envelope.

## 7. Errors

Exported sentinels in `internal/ghostboard`, so the controller can branch with `errors.Is`:

| Sentinel | Meaning | Later status |
| -------- | ------- | ----------- |
| `ErrBoardNotSealed` | `Open` on a plain board or a sealed board without `burn` | `409` |
| `ErrBoardBurned` | The name is a live tombstone | `410` |
| `ErrBurnRequiresOpen` | Plain read or stream of an unopened burn board | `409` |
| `ErrPlaintextWriteToSealed` | `Update` on a sealed board | `409` |
| `ErrSealedOverPlainText` | `UpdateSealed` on a plain board that holds text | `409` |
| `ErrSealedBoardFiles` | Attachment operation on a sealed board | `409` |
| `ErrSealedDisabled` | Sealed operation with `sealed_enabled: false` | `404` |
| `ErrBoardNotFound` | Unknown board for `Open`; expired tombstone | `404` |

The envelope errors come from `internal/sealedcrypto` (`ErrInvalidEnvelope`,
`ErrUnsupportedVersion`) and map to `400`; `ErrInvalidKey` never reaches the server, the key never
travels.

## 8. Tests

Named to match the plan's verification section.

- `internal/sealedcrypto/sealedcrypto_test.go`
  round trip with an empty plaintext, a multi-byte UTF-8 text and a text at the size limit; a
  flipped ciphertext byte; a wrong key; a wrong board name (AAD); an envelope with `v: 2`; a
  truncated or non-base64 `iv`/`ct`; `ParseKey` with base64url, hex, a full link, a 31-byte value,
  a 33-byte value and an invalid alphabet; `EnvelopeLen`/`MaxPlaintextBytes` inversion table.
- `internal/sealedcrypto/vector_test.go`
  loads `ui/assets/sealed-vector.json`, reproduces it byte for byte with the fixed IV and opens it.
  Fails when the file is missing, so a deleted vector cannot pass silently.
- `internal/ghostboard/sealed_test.go`
  creation as sealed; `Burn` fixed at creation; plaintext write refused; sealed write over a
  plain board with text refused; sealed write over an empty plain board accepted; malformed
  envelope refused before any state change; oversized envelope refused; identical envelope is a
  no-op; sealed read returns the envelope; plain read of an unopened burn board refused; stream of
  an unopened burn board refused; **exactly one success out of 16 concurrent `Open` callers** and
  the other 15 receive `ErrBoardBurned`; reopen after the burn refused; burn survives a store
  reload built from the same file; a flush failure leaves the board open and readable on retry
  (a read-only directory or an unwritable path); tombstone expiry frees the name and answers
  `ErrBoardNotFound`; an unopened burn board outlives `clear_delay` and is destroyed after the
  tombstone window; a sealed board's idle clear keeps `Mode`; attachment operations on a sealed
  board refused; `sealed_enabled: false` refuses writes while reads still work.
- `internal/config/config_test.go`
  the two defaults, the environment overrides, a zero `sealed_burn_tombstone_secs` rejected, and
  `WithDefaults` filling a zero-valued config.
- No test touches the network, the Docker socket or the clock beyond short timeouts.

## 9. Verification

- `task check` (fmt, vet, staticcheck, golangci-lint, tests) and `task build`.
- `go test -race ./internal/ghostboard` for the concurrency and burn path, unless the host is a
  Raspberry Pi 4 or another kernel that refuses the ThreadSanitizer VMA layout; then the limitation
  is stated in the summary instead of being reported as a pass.
- `go test -cover ./internal/sealedcrypto ./internal/ghostboard ./internal/config`: every touched
  package at or above the 70% recommendation, with the crypto and burn paths at 100% of their new
  branches. A package below the target is named in the summary with the reason.
- The envelope of a sealed write is grepped in a temporary `ghostboard.json`: the plaintext is
  absent and the envelope is the only thing in `text`.
- `go.mod` unchanged: the crypto is `crypto/aes`, `crypto/cipher` and `crypto/rand` only.

## 10. Acceptance criteria

1. `go test ./...` and `task check` pass, and the new tests above exist and fail if the behaviour
   they cover is removed.
2. A board created with `UpdateSealed` has `mode: "sealed"` and stores the envelope in `text`; an
   existing `ghostboard.json` without `mode` loads as plain and behaves exactly as today.
3. A plaintext write to a sealed board returns `ErrPlaintextWriteToSealed` and leaves the envelope
   untouched.
4. `Open` on a burn board returns the envelope once; every later call, in the same process or
   after a reload, returns `ErrBoardBurned` until the tombstone expires, then `ErrBoardNotFound`.
5. Under 16 concurrent `Open` calls exactly one receives the envelope.
6. A failing flush during `Open` returns an error and no envelope, and the board is still openable
   afterwards.
7. `Open` on a plain board or a sealed non-burn board returns `ErrBoardNotSealed`; `Get`/`Subscribe`
   on an unopened burn board returns `ErrBurnRequiresOpen`.
8. Attachment operations on a sealed board return `ErrSealedBoardFiles`.
9. `ghostboard.sealed_enabled: false` refuses sealed writes and leaves sealed reads working.
10. The vector test proves the Go implementation matches `ui/assets/sealed-vector.json` byte for
    byte.
11. The plan marks T-001 ... T-006 done, notes that T-007 landed only as configuration, and carries
    the new date; nothing else in `docs/` changes.

## 10a. Implementation notes

Where the delivered code differs from the wording above, and why:

- **Configuration keys** were added together with the routes, so the README table, the YAML example
  and `.env.example` carry them: the gap listed under "Open points" below does not exist.
- **`WithDefaults` does not fill `SealedEnabled`.** A bool cannot tell "unset" from an explicit
  false, and the existing `FilesEnabled` has the same shape, so `LoadConfig` supplies the default
  through viper and `WithDefaults` only fills `SealedBurnTombstone`.
- **`Store.Open` stays available when `sealed_enabled` is false**, as decided here: only the write
  path is refused (`ErrSealedDisabled`), so turning the feature off never strands a stored note.
- **`Open` on an empty burn board answers `ErrBoardNotFound`** and does not burn it, so a name is
  never reserved for a note that carries nothing.
- **The tombstone is a sibling array** (`"tombstones":[{"name","burned_at"}]`), and a name that is
  both a live board and a tombstone at load keeps the tombstone: a crash in the middle of a burn
  must never resurrect the note.
- **`Envelope.MarshalJSON`** spells out the wire order `{"v":1,"iv":…,"ct":…}`: the struct layout
  keeps the strings first for alignment, and the vector pins the wire form.
- **The idle window of an unopened burn board** is `sealed_burn_tombstone_secs`; when it elapses the
  envelope is destroyed and a tombstone recorded, so the note is gone and the name answers `410`
  for the usual window.

## 11. Open points

- ~~**Configuration keys without README rows.**~~ Resolved: the feature landed with T-015, so the
  README configuration table, the YAML example and `.env.example` carry both keys and the two
  `GO_SPIN_GHOSTBOARD_SEALED_*` variables.
- ~~**`.env.example`.**~~ Resolved with the keys above.
- **Idle expiry of an unopened burn board** (destroy the envelope and record the tombstone) is
  decided here and is not written in ADR-0010. If it should instead expire silently without a
  tombstone, the change is confined to `clearIfIdle`.
- **`Open` on an empty burn board** answers `ErrBoardNotFound` rather than burning: burning an
  empty note would tell a caller nothing and reserve a name for no reason. Worth a line in the
  README when the routes land.
- **Vector file placement under `ui/assets`** makes the vector publicly fetchable with a one-year
  cache. Acceptable for a published test key; the test guard described in 5.4 is what keeps a real
  key out.

## 12. Task list

| ID | Work | Status |
| -- | ---- | ------ |
| S-01 | `internal/sealedcrypto`: key, envelope, `Seal`/`Open`, `ParseKey`, `ParseEnvelope`, size accounting | to do |
| S-02 | `ui/assets/sealed-vector.json` and the vector test | to do |
| S-03 | `internal/ghostboard`: `Mode`, `Burn`, `SealedWrite`, `UpdateSealed`, refusals | to do |
| S-04 | `Store.Open`, tombstones, sync flush with rollback, load/flush of the tombstone array | to do |
| S-05 | Idle clear of burn boards and the `clear_delay` exemption | to do |
| S-06 | `internal/config`: two keys, defaults, validation, `config.yaml`, tests | to do |
| S-07 | Store, crypto and config tests from section 8 | to do |
| S-08 | Plan update: T-001 ... T-006 done, T-007 noted as configuration only, new date | to do |

## 13. References

- [ADR-0010 - Zero-knowledge ghostboards with the key in the fragment](../ADRs/0010-ghostboard-zero-knowledge-boards.md)
- [Plan - Zero-knowledge ghostboards](zero-knowledge-ghostboard.md)
- [ADR-0002 - Server-Sent Events for the ghostboard](../ADRs/0002-ghostboard-sse.md)
- [ADR-0008 - PIN protection for private ghostboards](../ADRs/0008-ghostboard-pin-protection.md)
- [ADR-0009 - Board-scoped file attachments](../ADRs/0009-ghostboard-file-attachments.md)
- [board-uploads-spec.md](board-uploads-spec.md): the shape an implemented board feature takes in
  this repository.
