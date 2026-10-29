# gb-cli

`gb-cli` is the command-line client of the GoSpin ghostboards (`cmd/board`, client in
`internal/boardclient`). It talks to the board HTTP API, so it works against a local server, a
container or a remote host and never needs access to the server files.

## Build and install

```bash
task build:cli      # -> .build/gb-cli, same version metadata as the server
go run ./cmd/board  # during development
```

`gb-cli version` reports the tag, commit, build time and Go toolchain baked into the binary;
`go run ./cmd/board version` reports the compiled defaults instead.

The help is inline (`gb-cli -h`, `gb-cli --help` or `gb-cli help`) and always names the executable as
it was invoked: a copy renamed to `my-board` prints `my-board <command> ...` in the usage and in its
error messages, so no example refers to a name the binary no longer has.

## Commands

| Command | Description |
| ------- | ----------- |
| `add [text]` | Append text to a board (a newline separates it from the existing text) |
| `set [text]` | Replace the whole text of a board (an empty text clears it) |
| `read` | Print the text of a board (`--stat`, `--json`) |
| `upload <file>` | Load a board from a file (`-` reads stdin), `--append` to append |
| `download [file]` | Save a board to a file (`<board>.txt` by default, `-` writes stdout, `--force` overwrites) |
| `follow` | Print the board and every change as it happens (`--diff`, Ctrl-C to stop) |
| `edit` | Edit a board with `$VISUAL`/`$EDITOR` and write it back |
| `config` | Print the configuration in use (`--paths`, `--json`); no request is made |
| `pin-hash [pin]` | Print the `ghostboard.pin_hmac` value of a PIN (argument or stdin) |
| `completion <shell>` | Print a completion script for `bash`, `zsh` or `fish` |
| `version` | Print the build metadata (also `--version`, `--json`) |

Every command takes `-b <board>`; without it the board from the configuration is used (`default`).

## Examples

```bash
# Append to the default board
gb-cli add "note from the shell"

# Several sources in one call: a pipe, files, or repeated -f
printf 'build finished\n' | gb-cli add -b lavoro
gb-cli add -b lavoro -f note.txt -f more.txt

# Replace (and clear) the text, previewing the change first
gb-cli set -b lavoro "nuovo contenuto"
gb-cli set -b lavoro "" --dry-run
gb-cli set -b lavoro ""

# Read it back, with the revision or as JSON
gb-cli read -b lavoro
gb-cli read -b lavoro --stat
gb-cli read -b lavoro --json

# Save it, load it, edit it
gb-cli download -b lavoro                 # writes lavoro.txt
gb-cli download -b lavoro -o /tmp/copia.txt --force
cat note.txt | gb-cli upload - -b lavoro
gb-cli upload -b lavoro --append ./more.txt
gb-cli edit -b lavoro

# Watch a board live: the whole text, or only what is appended
gb-cli follow -b lavoro
gb-cli follow -b lavoro --diff

# Script-friendly output and quiet mode
gb-cli --json add -b lavoro "deploy ok"
gb-cli -q add -b lavoro "deploy ok"

# Diagnostics: what would the CLI use, and which files does it look for?
gb-cli config
gb-cli config --paths
gb-cli version

# Shell completion
gb-cli completion bash > /etc/bash_completion.d/gb-cli
```

`gb-cli config` prints:

```
config file:  /home/me/.config/ghostboard/config.yaml
server url:   https://gospin.lan.asga.uk
board:        default
timeout:      30s
pin:          not set (public boards only)
executable:   /usr/local/bin/gb-cli
```

## Output, flags and exit codes

- `--json` writes one JSON object on stdout and nothing on stderr: the board state for `read`, the
  configuration for `config`, the build metadata for `version`, and
  `board`/`action`/`bytes`/`total_bytes`/`source`/`path`/`dry_run` for a mutation. A text stream and a
  JSON object never share stdout, so `download -` and `follow` refuse `--json` (use `read --json` for
  one revision) and `read --json` refuses `--stat`, whose summary the object already carries.
- `-q/--quiet` drops the confirmations on stderr while keeping the errors.
- `read`, `download -` and `follow` write the board text on stdout; `--stat` adds a one-line revision
  summary on stderr, so the text stays pipeable. A board that was never written has no update time:
  `read --stat` prints the revision alone and `read --json` omits `updated_at`, instead of inventing a
  date in year 1.
- `--dry-run` (`add`, `set`, `upload`) reports the bytes that would be written and leaves the board
  untouched; `add --dry-run` reads the board once to compute the exact result.
- `--retry <n>` retries a transient failure (`429`, `502`, `503`, `504` and network errors) with an
  exponential backoff. `--timeout <duration>` sets the per-request timeout.
- `--header "Name: Value"` adds an HTTP header (repeatable) and `GO_SPIN_BOARD_TOKEN` is sent as
  `Authorization: Bearer <token>`, for a board behind an authenticating proxy.
- Flags may be written before or after the positional arguments. A text that starts with `-` must
  follow `--` (`gb-cli add -- "-2 righe"`); `-f` and stdin are the other ways to pass it.
- The help, `version`, `config` and `completion` never contact the server.
- Exit codes: `0` success, `1` runtime or server error (including `413`), `2` misuse of the command
  line (a conflicting source such as an argument plus `-f` or `-o` plus a file argument, `--stat`
  with `--json`, a negative `--retry`, a non-positive `--timeout`), `3` the URL does not expose the
  board API, `4` invalid board name.

## Behaviour notes

- `add` is a read followed by a write: a concurrent writer in between can be overwritten, so use
  `set` when the board must end up with an exact content.
- `add` puts a newline before the new text when the board does not already end with one, and joins
  repeated `-f` files the same way, so neither the last line of the board nor the last line of a file
  glues onto what follows.
- `follow` prints the whole text on every change; `--diff` prints only what the revision added (a
  revision that is not an extension of the previous one is printed in full). Ctrl-C exits `0`.
- `download` takes its target either with `-o` or as the argument, never both; a target `-` writes
  stdout. It refuses to overwrite an existing file without `--force`.
- `upload` refuses a file that is not valid UTF-8 before contacting the server, naming the file.
- `edit` writes the board to a temporary file, runs `$VISUAL` (then `$EDITOR`) on it and writes the
  result back; an unchanged file is not written at all. An editor value with arguments (`code -w`) is
  split on whitespace, so it cannot carry quoted arguments.
- A `404 Not Found` on a board route means the URL does not point at a GoSpin server (wrong port, host
  or path prefix): the CLI names the missing route and the full request URL instead of quoting the web
  page, and never prints an HTML body as an error.
- Board names are validated before a request is made, so a malformed name never reaches the server.

## Configuration

Precedence, lowest first: built-in defaults (`http://localhost:8084`, board `default`, 30s timeout),
then a YAML file, then the environment, then the flags.

```yaml
# config.yaml
# Prefer GO_SPIN_BOARD_PIN over this key: the file is a file on disk.
url: "http://localhost:8084"   # server root (or reverse-proxy prefix), never a /ui page
board: "default"               # board used when a command does not name one
pin: "1234"                    # PIN of a private board (4-64 characters, optional)
timeout_secs: 30               # per-request timeout
```

`url` is the base the CLI appends the API paths to (`<url>/api/boards/<board>`,
`<url>/ghostboard/<board>/events`), so it must be the server root — or the prefix a reverse proxy
mounts GoSpin on. A UI page such as `https://gospin.lan.asga.uk/ui/dashboard/` is rejected with an
explanation instead of being used as an API prefix (which would only ever answer `404`).

The file is looked up next to the executable first (portable installs) and then in
`$XDG_CONFIG_HOME/ghostboard/config.yaml`, falling back to `~/.config/ghostboard/config.yaml`.

| Flag | Effect |
| ---- | ------ |
| `--config <path>` | Use this file; it must exist, and the last occurrence wins |
| `--no-config` | Ignore every configuration file for this invocation |

| Environment variable | Effect |
| -------------------- | ------ |
| `GO_SPIN_BOARD_URL` | Overrides `url` |
| `GO_SPIN_BOARD_PIN` | Overrides `pin`; prefer it to `--pin`, which stays in the shell history and in `ps` |
| `GO_SPIN_BOARD_TIMEOUT_SECS` | Overrides `timeout_secs` |
| `GO_SPIN_BOARD_TOKEN` | Sent as `Authorization: Bearer <token>` |
| `GO_SPIN_GHOSTBOARD_PIN_SECRET` | Secret `pin-hash` uses to compute the digest (never sent anywhere) |

A key the CLI does not know (`timeout_sec` instead of `timeout_secs`) is reported by `config` as a
warning instead of being silently ignored, and `config` also lists the environment variables that
replaced a file value, so a typo can never look like a default that was never overridden.

## Server endpoints used

| Method and path | Purpose |
| --------------- | ------- |
| `GET /api/boards/:board` | Board text as `text/plain` (empty `200` when never written) |
| `POST /api/boards/:board` | Replace the text with the raw body, byte for byte; `204 No Content` |
| `GET /ghostboard/:board` | JSON board state with revision and update time (`read --stat`, `read --json`) |
| `GET /ghostboard/:board/events` | Server-Sent Events stream consumed by `follow` |
| `POST /ghostboard/:board/unlock` | Trades the PIN for a session token; called by itself on the first `401` |

## Private boards

A board listed in `ghostboard.private_boards` on the server needs its PIN for reading and writing.
Give the CLI the PIN and it takes care of the rest: on the first `401` it posts the PIN to
`/ghostboard/:board/unlock`, keeps the returned session token and sends it as a bearer token for
the remaining requests of the same command, so the PIN travels once and a rejection is never
retried.

```bash
# One command, PIN inline (stays in the shell history: prefer the environment)
gb-cli read -b lavoro --pin 1234

# Or export it once for the session
export GO_SPIN_BOARD_PIN=1234
gb-cli follow -b lavoro

# A server-side digest for ghostboard.pin_hmac, with the same secret the server will use
export GO_SPIN_GHOSTBOARD_PIN_SECRET="$(openssl rand -hex 32)"
gb-cli pin-hash 1234
```

| Situation | Exit code | Message |
| --------- | --------- | ------- |
| No PIN configured for a private board | `1` | `board "x" is private: pass its pin with --pin, ...` |
| Wrong PIN | `1` | `board "x": the pin was rejected` |

A PIN that is not 4-64 characters is rejected before any request is made, like an invalid board
name. `gb-cli pin-hash` prints the HMAC-SHA256 of the PIN under `GO_SPIN_GHOSTBOARD_PIN_SECRET`,
which is exactly what `ghostboard.pin_hmac` expects; the secret must stay out of the configuration
file and match the one the server runs with.

The board rules — name validation, the `ghostboard.max_text_bytes` limit, the idle clear and the JSON
persistence — are described in [README.md - Ghostboard](../README.md#ghostboard) and in
[ARCHITECTURE.md - Ghostboard Store](ARCHITECTURE.md#ghostboard-store).
