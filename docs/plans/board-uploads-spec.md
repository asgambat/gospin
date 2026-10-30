# Ghostboard file attachments - specification

**Status**: implemented (2026-10-30)
**Last updated**: 2026-10-30
**Short name**: `board-uploads`

## 1. Context and goal

The ghostboard is a shared text surface: one text per board, live over SSE, optionally protected by
a PIN (ADR-0002, ADR-0008). It cannot carry a screenshot, a PDF or an archive, so the workflow it
replaces (AirDrop, LocalSend, a chat with yourself) still needs another tool for anything that is
not text. This change adds **file attachments to a board**: upload from the page or from `gb-cli`,
list them, download them, delete them, with the PIN of a private board covering them.

The feature is deliberately smaller than a file-sharing app: no account system, no folders, no
renaming, no thumbnails, no resumable uploads. It is the smallest addition that makes a board usable
to move a 40 MB screenshot from a phone to a laptop.

"Done" means: a user opens `/ui/ghostboard/lavoro`, drops a file, sees it in the panel, downloads it
from another device, and the same file can be uploaded, listed and fetched with `gb-cli`, on a board
that works offline and behind the existing PIN.

## 2. Decisions taken in the interview

| Topic | Decision |
| ----- | -------- |
| Ownership | Files belong to a board (`/ghostboard/:board/...`). No global list, no cross-board browsing. |
| Lifecycle | Files follow the board text: the idle clear deletes text **and** files; `file_ttl_secs` is a hard cap for boards kept alive by open pages or activity. |
| Activity | Upload and delete count as board activity and re-arm the idle clear. |
| Default TTL | 24 hours (`file_ttl_secs = 86400`). |
| Limits | Per file, per board, per board count and instance-wide: `max_file_bytes`, `max_board_file_bytes`, `max_file_count`, `max_total_file_bytes`. |
| Limit values | 800 MiB per file, 2 GiB per board, 10 files per board, 5 GiB per instance. |
| Storage path | Derived next to `ghostboard.json`: `<dir of ghostboard.file_path>/files/<board>/<id>`. No extra key. |
| Enablement | On by default with the limits above. |
| Config naming | Flat keys with a `file_` prefix under `ghostboard.` |
| Metadata | Inside `ghostboard.json`, under each board (`files` array), reusing flush and dirty tracking. |
| Startup | Reconcile: delete files without metadata, drop metadata without a file, prune unknown board directories. Symlinks are never followed. |
| API surfaces | `/ghostboard/:board/files...` returns JSON; `/api/boards/:board/files...` is the script surface with a plain-text list. Both implement the same operations. |
| Upload batching | Exactly one file per request; a multi-file drop issues N requests. |
| File identity | Random 128-bit id, base32 (lowercase, no padding, 26 characters), used as the on-disk name. |
| File name | Original name is a download label only; sanitized for headers and display. No rename, no user-supplied label. |
| Serving | Always `Content-Disposition: attachment`, `application/octet-stream`, never inline. |
| Listing on a private board | Protected like the text: `401` without a valid session, no names or sizes leak. |
| Transfer timeout | `file_transfer_timeout_secs = 600`, applied per upload/download request. |
| UI placement | Collapsible panel above the textarea, always in the page. No lifetime countdown. |
| Upload progress | Per-file percentage with cancel (XMLHttpRequest). |
| Delete | Per-file confirmation via `window.confirm`; anyone with the session can delete. |
| Clear button | Asks in a dialog: clear text only, or clear text and files. |
| CLI | New subcommands `file upload`, `file list`, `file get`, `file delete` (the existing `upload`/`download` keep meaning board text). |
| Scope | One change: server, UI, CLI, tests, docs and ADR-0009. |
| Spec location | `docs/plans/board-uploads-spec.md` (this file). |

### Consequence to state loudly in the docs

With `clear_delay_secs = 300` (default) and `file_ttl_secs = 86400`, files usually disappear with the
text about **five minutes after the last activity on the board**, not after a day. The 24-hour cap
only applies while the board is kept alive (an open page, or uploads/deletes within the window). A
file uploaded by a script with no browser open is deleted when the idle clear fires. This is the
"one sharing session" model the board already has; the README must say it in the section that
introduces attachments.

An idle clear happens only while no client is subscribed: a page left open keeps the board (and its
files) alive, which is what makes the 24-hour cap reachable.

## 3. Scope

### In

- `internal/config`: seven new `ghostboard` keys, defaults, validation, tests.
- `internal/ghostboard`: file metadata in the store, quota checks, TTL sweep, idle clear deletes
  files, startup reconciliation, `files` in the persisted document and in board payloads.
- `internal/api/controller/ghostboard_controller.go`: list, upload, download, delete one, delete all.
- `internal/api/route/ghostboard_route.go`: file routes on both surfaces, PIN gate, timeout bypass.
- `internal/api/middleware`: no change beyond reusing `GhostboardPIN`.
- `internal/boardclient` + `cmd/board`: `file upload`, `file list`, `file get`.
- `ui/ghostboard.html`, `ui/assets/ghostboard.js`: panel, drop zone, progress, delete, Clear dialog.
- `config/config.yaml`, `.gitignore` (`config/data/files/`), `docs/gospin.postman_collection.json`.
- Documentation: README, ARCHITECTURE, FRONTEND, CLI, CHANGELOG, ADR-0009, `docs/README.md` index,
  screenshots fixtures + regenerated `docs/screenshots/ghostboard.png`.

### Out

- Rename, edit or user-provided labels; folders; a global "all files" page.
- Inline preview/thumbnails, image transcoding, virus scanning, dedup, compression, encryption.
- Resumable/chunked uploads, folder upload, background queue, retries on the server side.
- Per-file ACLs, signed per-file URLs, per-board PINs, accounts.
- External storage (S3, WebDAV), metrics endpoints, retention policies beyond the two TTLs.
- A CLI command that deletes every file of a board: `file delete` removes one id, and the bulk
  delete stays the Clear action of the page.

## 4. Configuration

New keys under `ghostboard`, all with a `GO_SPIN_` override following the existing rule
(`ghostboard.max_file_bytes` → `GO_SPIN_GHOSTBOARD_MAX_FILE_BYTES`):

| Key | Default | Meaning |
| --- | ------- | ------- |
| `files_enabled` | `true` | Registers the file routes and accepts uploads. When `false` every file endpoint answers `404` and the panel is hidden. |
| `max_file_bytes` | `838860800` (800 MiB) | Largest single upload. Also caps the request body (`max_file_bytes` + 1 MiB of multipart overhead). |
| `max_file_count` | `10` | Files per board. |
| `max_board_file_bytes` | `2147483648` (2 GiB) | Total bytes of the files of one board. |
| `max_total_file_bytes` | `5368709120` (5 GiB) | Total bytes of all attachments in the instance. |
| `file_ttl_secs` | `86400` | Hard cap on a file's life, measured from its upload. |
| `file_transfer_timeout_secs` | `600` | Read deadline of an upload and write deadline of a download. |

Validation at startup (fail fast, message names the key):

- every value must be positive;
- `max_file_bytes <= max_board_file_bytes <= max_total_file_bytes`;
- `max_file_count >= 1`;
- values are validated even when `files_enabled` is `false`, so enabling later cannot fail at runtime.

`config/config.yaml` gets the seven keys with comments that mention the idle-clear interaction.
README gets the same rows in the environment-variable tables.

## 5. Data model

### On disk

```text
<dir of ghostboard.file_path>/files/<board>/<id>
```

- `<board>` is the already validated board name (1-64 letters, digits, dots, dashes, underscores).
- `<id>` is 128 random bits encoded as lowercase base32 without padding: `^[a-z2-7]{26}$`.
- Files are written to `<id>.tmp` in the same directory and renamed into place; mode `0600`, board
  directories `0755`, matching the store's existing permissions.
- The original name never reaches the filesystem.

### In `ghostboard.json`

Backwards compatible addition to each board object:

```json
{
  "boards": [
    {
      "updated_at": "2026-10-29T15:04:05Z",
      "name": "lavoro",
      "text": "…",
      "revision": 4,
      "files": [
        {
          "id": "mfrggzdfmztwq2lknnwg23tpob",
          "name": "report.pdf",
          "size": 1843200,
          "content_type": "application/pdf",
          "created_at": "2026-10-29T15:04:05Z",
          "expires_at": "2026-10-30T15:04:05Z"
        }
      ]
    }
  ]
}
```

- A board without `files` loads as a board with no attachments (old files remain readable).
- `content_type` is detected with `http.DetectContentType` on the first 512 bytes and is informational
  only: downloads are always `application/octet-stream`.
- `expires_at = created_at + file_ttl_secs`. It is a cap, not a promise: the idle clear can delete the
  file earlier.
- Files are listed newest first.

### Store API (`internal/ghostboard`)

```go
type File struct {
    CreatedAt   time.Time
    ExpiresAt   time.Time
    ID          string
    Name        string
    ContentType string
    Size        int64
}

// AddFile commits an already written temporary file, enforcing count and byte
// quotas under the store lock.
func (s *Store) AddFile(board, name, contentType string, tempPath string, size int64) (File, error)

func (s *Store) Files(board string) ([]File, error)
func (s *Store) File(board, id string) (File, error)
func (s *Store) DeleteFile(board, id string) error
func (s *Store) DeleteFiles(board string) (int, error)
func (s *Store) SweepFiles(now time.Time) (int, error)
func (s *Store) ReconcileFiles(root string) error
```

New errors: `ErrFileNotFound`, `ErrFileTooLarge`, `ErrFileCountExceeded`,
`ErrBoardFileQuotaExceeded`, `ErrTotalFileQuotaExceeded`. Each maps to a stable HTTP status (section 6).

## 6. HTTP API

Both surfaces are registered only when `files_enabled`; all of them sit behind
`middleware.GhostboardPIN`, exactly like the text routes. The per-request timeout middleware is
**not** applied to upload/download; those handlers set their own read/write deadline with
`http.NewResponseController` to `file_transfer_timeout_secs`, because the 10-second server timeout
would cut an 800 MiB transfer. When the deadline passes, the handler answers `504` if nothing was
written yet, otherwise closes the stream.

### `/ghostboard/:board/files` (JSON)

| Method | Path | Behaviour |
| ------ | ---- | --------- |
| GET | `/ghostboard/:board/files` | `200 {"files":[File…]}` |
| POST | `/ghostboard/:board/files` | Multipart with exactly one part named `file`; `201` with the created `File` |
| GET | `/ghostboard/:board/files/:id` | `200` with the bytes |
| DELETE | `/ghostboard/:board/files/:id` | `204`, `404` when the id is unknown |
| DELETE | `/ghostboard/:board/files` | `204`, deletes every file of the board (Clear dialog) |

`GET /ghostboard/:board` gains a `files` field with the same array, and every SSE `board` event
carries it, so the page does not need a second request to stay in sync. Adding or deleting a file
broadcasts a board event **without** touching `revision` or `updated_at`; the event carries the
client id of the uploader, whose tab updates from its own response and ignores the echo. A board
that holds only attachments therefore still reports the zero time, which the page reads as no time
at all and labels "Files only, no text yet" instead of a save time of year 1.

### `/api/boards/:board/files` (plain text for scripts)

Same operations; the differences are representation:

| Method | Path | Behaviour |
| ------ | ---- | --------- |
| GET | `/api/boards/:board/files` | `200 text/plain` with one tab-separated line per file: `id\tname\tsize_bytes\texpires_at` |
| POST | `/api/boards/:board/files` | `201 text/plain` with the single line of the created file |
| GET | `/api/boards/:board/files/:id` | `200` with the bytes |
| DELETE | `/api/boards/:board/files/:id` | `204` |
| DELETE | `/api/boards/:board/files` | `204` |

Tabs, CR and LF in a name are replaced with spaces before a line is rendered, so a hostile file name
cannot forge a line.

### Upload details

- Content type must be `multipart/form-data`; anything else is `415`.
- The body is capped with `http.MaxBytesReader` at `max_file_bytes + 1 MiB`; exceeding it is `413`.
- Only the first part named `file` is streamed to `<board>/.tmp-<id>` with `io.CopyN`; leftover parts
  are drained. A missing part is `400`.
- An empty or missing multipart filename is `400`. The name is trimmed, control characters and
  `\r\n\t` are removed, it is capped at 200 runes, and falls back to `file` when nothing is left.
- Quotas are checked **after** the bytes are on disk and **before** the rename, under the store lock
  (this is also the reservation: a rejected upload deletes its temp file), so two concurrent uploads
  cannot both pass a nearly full board.
- `SweepFiles` runs before the check, so an expired file frees its quota space immediately.
- Upload and delete call the same activity hook as a text write, re-arming the idle clear.
- The temp file is removed if the client aborts, if the request deadline fires, or on any error.

### Download details

```text
Content-Type: application/octet-stream
Content-Disposition: attachment; filename="<ascii fallback>"; filename*=UTF-8''<rfc5987 name>
Content-Length: <size>
X-Content-Type-Options: nosniff
Cache-Control: no-store
Content-Security-Policy: sandbox
```

`http.ServeContent` is used after seeking to 0, so `Range` works for resumable downloads and large
files without loading them in memory. The file is opened by joining `<root>/<board>/<id>` and then
verifying the result is still under the root; a symlink is refused (`400`).

### Status and error envelope

Errors stay JSON on both surfaces, like the rest of the ghostboard API:

| Status | When | Body |
| ------ | ---- | ---- |
| 400 | invalid board or id, missing/empty `file` part, empty file, symlink, malformed multipart | `{"error":"…"}` |
| 401 | private board without a valid session | `{"error":"pin required","board":"…"}` (existing middleware) |
| 404 | unknown file id, or any file route while `files_enabled=false` | `{"error":"file not found"}` |
| 409 | `max_file_count` reached | `{"error":"too many files on this board","limit":"max_file_count","limit_value":10}` |
| 413 | file over `max_file_bytes`, board over `max_board_file_bytes`, instance over `max_total_file_bytes` | `{"error":"…","limit":"max_file_bytes","limit_bytes":838860800}` |
| 415 | not multipart | `{"error":"expected multipart/form-data"}` |
| 507 | the disk rejected the write (`ENOSPC`) | `{"error":"not enough storage"}` |
| 504 | transfer deadline hit before the response started | `{"error":"transfer timeout"}` |

Rejections are logged at `Warn` with board, id and reason; accepted uploads and deletes at `Info` with
board, id and size. Contents are never logged.

## 7. Lifecycle

1. **Upload** → temp file → quota check → rename → metadata in memory → dirty → board event →
   idle-clear timer re-armed.
2. **Idle clear** (`clearIfIdle`, unchanged trigger): deletes the files of the board from disk and
   from the metadata, clears the text, increments `revision`, broadcasts, logs at `Debug`. If deleting
   a file fails, the metadata entry is still dropped and the failure is logged at `Error`, so the
   board never blocks on a stubborn file.
3. **TTL sweep** (`SweepFiles`) runs at startup, on every persistence tick (existing 2 s), and before
   any quota check. It deletes expired files only; it never touches the text.
4. **Explicit clear** from the page:
   - "Clear text only": `POST /ghostboard/:board` with an empty text. Files stay until the TTL.
   - "Clear text and files": `DELETE /ghostboard/:board/files` then the empty text.
   - Emptying the text by editing is the "text only" case: it does not delete files.
5. **Explicit delete** from the panel: `DELETE /ghostboard/:board/files/:id`.
6. **Startup reconciliation**, before serving:
   - metadata entry whose file is missing → drop it, log `Warn`;
   - regular file in a board directory without a metadata entry → delete, log `Warn`;
   - unknown board directory → delete its entries, log `Warn`;
   - symlink or non-regular file → delete the link, never follow it, log `Warn`.

Metadata is persisted by the existing flush loop, so a crash can lose the metadata of the last
seconds while the bytes are already on disk; the next startup removes those orphans. That trade-off
is intentional: the alternative (fsync per upload before answering) costs more than it protects on
an ephemeral board.

## 8. UI

`ui/ghostboard.html` gains a panel **above** the textarea, inside the existing board section:

- Header: a toggle button `📎 Files (N)` with `aria-expanded`, collapsed by default when there are no
  files and expanded when there is at least one. The panel stays available in Preview mode (it is
  outside the text/preview swap).
- Drop zone: click opens the file picker, drag&drop accepts several files; each file is uploaded by
  its own `XMLHttpRequest` (needed for `upload.onprogress`), one after the other.
- Each in-flight file is a row with its name, a progress bar (percentage), and a cancel button that
  calls `xhr.abort()`.
- The list shows name, size in human units, download link and a delete button with
  `window.confirm('Delete "<name>" from this board?')`. No thumbnails, no lifetime countdown.
- Names are rendered with `x-text` and are never inserted as HTML.
- Failures show the server message (limit reached, too large, session expired). A `401` on upload
  reuses `sessionExpired()` like a save; a `413`/`409` shows the limit, not a generic error.
- The page gets `data-max-file-bytes` injected by `serveGhostboardPage`, and the client refuses a file
  bigger than that before sending it, mirroring the existing `data-max-bytes` check for text files.
- The Clear button now opens a small inline confirmation with two choices ("Clear text only",
  "Clear text and files") instead of `window.confirm`; both call the endpoints of section 7.
- The panel is hidden while the page is locked, and `showLock()` clears the file list it had.

`ui/assets/ghostboard.js` gains `files`, `uploading`, `filesOpen` state, `uploadFiles`, `uploadOne`
(XHR + progress + abort), `deleteFile`, `clearText`, `clearTextAndFiles`, and applies `payload.files`
in `applyPayload()`.

## 9. `gb-cli`

| Command | Behaviour |
| ------- | --------- |
| `gb-cli file upload <path> [--board B] [--pin P] [--json] [-q]` | Uploads one file with multipart; prints `id`, `name`, `size` (human) or `--json` `{id,name,size,expires_at}`; `-q` prints only the id. |
| `gb-cli file list [--board B] [--json]` | Aligned `ID NAME SIZE EXPIRES`; `--json` prints the array. |
| `gb-cli file get <id> [--board B] [-o path|-] [--json]` | Writes the bytes to stdout, or to `-o`; prints nothing on stdout when `-o` is a file. |
| `gb-cli file delete <id> [--board B] [--pin P] [--json] [-q]` | Deletes one file and confirms on stderr (`-q` silences it); `--json` prints `{board,id}`. An unknown id exits `1`. |

- Shared flags (`--pin`, `--url`, `--timeout`, `--header`) and the automatic unlock on `401` work as
  for the other commands; the session token is reused for the whole command.
- A `404` on `file get` or `file delete` is an `EndpointError` (exit 3) if the URL is wrong, and a
  missing id maps to exit 1 with `file <id> not found` — the client distinguishes the two by the
  response body.
- No new environment variables: `GO_SPIN_BOARD_*` already covers URL, PIN and timeout.
- `--json` and `-q` follow the conventions of the existing commands; `file list` and `file get`
  never mix text and JSON on stdout.

## 10. Security

- IDs are generated server-side; the path is built from a validated board name and an id matching
  `^[a-z2-7]{26}$`, then checked to stay under the files root. No user string reaches the path.
- All file routes go through `GhostboardPIN`; a private board answers `401` for names, sizes,
  downloads and deletions alike.
- Downloads are never inline, always `application/octet-stream` + `attachment` + `nosniff` + a
  sandbox CSP, so an uploaded HTML/SVG cannot run on the dashboard origin with the PIN session.
- `Content-Disposition` is built from a sanitized ASCII fallback plus an RFC 5987 `filename*`; CR, LF,
  quotes and control characters are removed, which prevents header injection.
- Uploads stream to disk; no `io.ReadAll`, no `ParseMultipartForm` buffering.
- Quotas are enforced under the store lock and re-checked after the bytes are written, so concurrent
  uploads cannot overshoot the board or instance cap.
- Filenames are untrusted text in the page: rendered with `x-text`, escaped by the JSON encoder.

## 11. Testing

New/updated tests, next to the code they cover:

- `internal/config/config_test.go`: defaults, env overrides, the ordering constraints, negative
  values, `files_enabled=false` still validating, and that `file_transfer_timeout_secs` accepts large
  values.
- `internal/ghostboard/store_test.go`: add/list/get/delete; count, board and total quotas; TTL sweep;
  idle clear deletes files and text together; upload re-arms the clear; reconciliation (orphan file,
  missing file, unknown directory, symlink); persistence round-trip with `files`; old document
  without `files`; `DeleteFiles` returns the count.
- `internal/api/controller/ghostboard_files_test.go`: multipart success (`201`, metadata, headers,
  bytes on disk), missing part, empty name, oversized file, count limit, board quota, total quota,
  `404` unknown id, delete one/all, download headers and `Range`, symlink refused, `files_enabled=false`
  answers `404`, JSON list shape. The quota branches and the path validation need full coverage.
- `internal/api/route/ghostboard_route_test.go`: every file endpoint is `401` without a session and
  works with one, on both surfaces; the plain listing is tab-separated with one line per file.
- `cmd/board/file_test.go` + `internal/boardclient/file_test.go`: upload/list/get against
  `httptest`, `--json`, `-o`, `-q`, unlock on `401`, exit codes for a missing id and a missing file.
- Playwright (temporary driver in `.build`, same as the previous UI checks): upload via
  `setInputFiles`, row appears with progress, download event fires, delete asks for confirmation,
  Clear dialog offers both choices and the second deletes the file, a private board hides the panel
  behind the PIN form, no console errors.
- `task lint`, `go test ./...`, `task test:race` (store concurrency and the upload path).

Coverage target: 70% per package, and every new branch of upload, quota and delete code covered.

## 12. Documentation and assets

- **README**: rows in the Ghostboard API table and in the environment-variable tables, a "File
  attachments" subsection with `curl` examples (upload, list, download, delete), the lifecycle
  warning from section 2, the Clear semantics, and a proxy note about `client_max_body_size`
  /`proxy_request_buffering` for large uploads.
- **docs/ARCHITECTURE.md**: files in the ghostboard store, the temp-then-commit upload, the quota
  model, the idle clear and reconciliation, the two API surfaces.
- **docs/FRONTEND.md**: panel, XHR progress, no inline preview, the `data-max-file-bytes` contract.
- **docs/CLI.md**: the three `file` subcommands, output shapes, exit codes.
- **CHANGELOG.md**: `[Unreleased]` entry.
- **ADR-0009**: board-scoped attachments, always-attachment serving, metadata in `ghostboard.json`,
  rejected alternatives (global list, inline preview, per-file PIN, separate metadata file, resumable
  uploads).
- **docs/README.md**: ADR-0009 row in the index table.
- **config/config.yaml**: the seven keys with comments.
- **.gitignore**: `config/data/files/` so attachments never show up in `git status`.
- **Postman collection**: the five requests of the JSON surface.
- **Screenshots**: one demo attachment in `scripts/screenshots/fixtures/`, panel visible in
  `docs/screenshots/ghostboard.png`, regenerated on a host with an emoji font.

## 13. Acceptance criteria

1. With defaults, dropping a file on a board shows it in the panel and makes it downloadable from
   another browser on the same board.
2. `gb-cli file upload`, `file list` and `file get` move the same file, on a public and a private
   board, without a browser.
3. A private board answers `401` to list, upload, download and delete without a session, and works
   with one.
4. Limits are enforced and mapped to `413`/`409` with the limiting key in the body; a rejected upload
   leaves no byte on disk.
5. Killing the server after an upload keeps the file and its metadata; a file removed by hand
   disappears from the board at the next start; an orphan file is removed at the next start.
6. The idle clear removes text and files together, and deletes them from disk.
7. `files_enabled=false` removes the routes (`404`) and hides the panel; the rest of the board works
   as before.
8. Downloads are `attachment` + `nosniff`, and an uploaded HTML file cannot run as a page.
9. An 800 MiB upload succeeds over a link slower than 22 Mbps (deadline of 10 minutes) and a
   cancelled upload leaves only a temp file, which is removed.
10. `task lint`, `go test ./...`, `task test:race` and the Playwright checks pass; README,
    ARCHITECTURE, FRONTEND, CLI, CHANGELOG and ADR-0009 are updated in the same change.

## 14. Task list

| ID | Task | Depends on | Complexity | Done |
| -- | ---- | ---------- | ---------- | ---- |
| T-001 | Config keys, defaults, validation, yaml + tests | – | M | yes |
| T-002 | Store: `File` type, quotas, TTL sweep, clear integration, reconciliation, persistence | T-001 | L | yes |
| T-003 | Controller: list, upload (stream), download, delete, headers, error mapping | T-002 | L | yes |
| T-004 | Routes on both surfaces, PIN gate, deadline handling, `files_enabled` | T-003 | S | yes |
| T-005 | Board payload and SSE events carry `files` | T-002 | S | yes |
| T-006 | UI panel, drop zone, progress, delete, Clear dialog | T-004 | L | yes |
| T-007 | `gb-cli file upload/list/get` + client methods + tests | T-004 | M | yes |
| T-008 | Playwright verification and screenshot fixture | T-006 | M | yes |
| T-009 | README, ARCHITECTURE, FRONTEND, CLI, CHANGELOG, ADR-0009, index, Postman | T-006, T-007 | M | yes |
| T-010 | `task check`, `task test:race`, final manual pass (curl + CLI + page) | all | S | yes |
| T-011 | Follow-on: `gb-cli file delete` + client method + tests | T-007 | S | yes |

Complexity scale: `XS`, `S`, `M`, `L`, `XL`.

### Verification record

Run on 2026-10-30 on the feature branch:

- `gofmt -l cmd internal`, `go vet ./...`, `task lint` (staticcheck + golangci-lint): clean.
- `go test ./... -count=1`: all packages pass.
- `go test -cover`: `internal/ghostboard` 84.4%, `internal/api/controller` 82.3%,
  `internal/api/route` 88.8%, `internal/api/middleware` 75.9%, `internal/boardclient` 86.4%,
  `cmd/board` 81.4%, `internal/config` 94.7% — all above the 70% target.
- `go test -race`: the sandbox kernel refuses the ThreadSanitizer VMA layout
  (`FATAL: ThreadSanitizer: unsupported VMA range`), so the race run could not be completed here.
- `task screenshots` with a fontconfig that provides Noto Color Emoji: `docs/screenshots/ghostboard.png`
  now shows the Files panel with the `demo-note.txt` fixture.
- Manual pass: upload, list, download, delete and the plain-text surface with `curl`; `gb-cli file
  upload/list/get` on a public and a PIN-protected board; Playwright over the page (drop, progress,
  download event, delete confirmation, Clear dialog, locked board).

## 15. Risks and mitigations

| Risk | Mitigation |
| ---- | ---------- |
| Disk exhaustion despite quotas | Global cap, per-board cap, TTL sweep, startup reconciliation; `ENOSPC` mapped to `507`. |
| A long upload blocks a request slot | Deadline of `file_transfer_timeout_secs`, streaming to disk, no whole-body buffering. |
| Crash between bytes and metadata | Orphans removed at startup; documented as a deliberate trade-off. |
| Proxy limits reject large uploads | README note with the `client_max_body_size` / buffering settings. |
| `config/data` is tracked in git | Explicit `.gitignore` entry for `config/data/files/`. |
| Files outliving their board text | Idle clear deletes both; the README states the 5-minute reality of the default configuration. |
| Filename injection in headers or UI | Sanitization + RFC 5987 encoding + `x-text` rendering. |
| Two concurrent uploads overshoot a quota | Quota check and commit under the store lock. |

## 16. References

- ADR-0002 - Server-Sent Events for the ghostboard.
- ADR-0006 - JSON data file with an in-memory cache and dirty tracking.
- ADR-0008 - PIN protection for private ghostboards.
- `docs/ARCHITECTURE.md` - Ghostboard Store and CLI sections.
- `docs/FRONTEND.md` - ghostboard page rules.
- `docs/CLI.md` - command conventions and exit codes.
- `docs/plans/plan-template.md` - structure of this document.
