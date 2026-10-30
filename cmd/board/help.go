package main

import (
	"fmt"
	"io"
	"strings"
)

// printUsage writes the help text, naming the executable as it was invoked so
// a renamed binary documents itself.
func printUsage(w io.Writer, program string) {
	help := strings.ReplaceAll(usageTemplate, programPlaceholder, program)
	if _, err := fmt.Fprint(w, help); err != nil {
		return
	}
}

// usageTemplate is the inline help. The programPlaceholder token is replaced
// with the invoked executable name, so every example follows a renamed binary.
const usageTemplate = `{program} reads and writes the shared text boards served by GoSpin.

Usage:
  {program} <command> [flags] [arguments]

Commands:
  add [text]        Append text to a board (a newline separates it from the old text)
  set [text]        Replace the whole text of a board
  read              Print the text of a board
  upload <file>     Load a board from a file (- reads stdin)
  download [file]   Save a board to a file (default <board>.txt, - writes stdout)
  follow            Print the board and every change as it happens (Ctrl-C to stop)
  edit              Edit a board with $VISUAL or $EDITOR and write it back
  file <action>     Work with the files of a board: upload, list, get or delete
  sealed new [text] Create a sealed board and print the link that holds its key
  open              Open a burn board once, destroying it, and print its text
  config            Print the configuration in use (no request is made)
  pin-hash [pin]    Print the ghostboard.pin_hmac value of a PIN
  completion <sh>   Print a completion script (bash, zsh or fish)
  version           Print the build metadata

Flags:
  -b, --board <name>     Board to use (default: from configuration, else "default")
  -u, --url <url>        GoSpin server URL (default: from configuration, else
                         http://localhost:8084)
      --pin <pin>        PIN of a board protected by ghostboard.private_boards;
                         prefer GO_SPIN_BOARD_PIN or the configuration file,
                         since a flag stays in the shell history and in ps
      --key <key>        Key of a sealed board: base64url, hex or a whole link;
                         prefer GO_SPIN_BOARD_KEY or --key-file, since a flag
                         stays in the shell history and in ps
      --key-file <path>  File holding the key of a sealed board (a trailing
                         newline is ignored)
      --burn             Destroy the board after the first open (sealed new,
                         on by default)
      --timeout <dur>    Request timeout, e.g. 30s or 1m
      --config <path>    Configuration file to use
      --no-config        Ignore the configuration files
      --retry <n>        Retry a transient failure (429/502/503/504) n times
      --header <header>  Extra HTTP header, "Name: Value", repeatable
  -q, --quiet            Suppress the confirmations on stderr
      --json             Write a machine-readable result on stdout
  -f <file>              File to read the text from, repeatable (add, set)
      --dry-run          Show what would change without writing (add, set, upload)
  -o <file>              Output file (download, file get)
      --force            Overwrite an existing file (download, file get)
      --all              Download every file of a board as one ZIP (file get)
      --stat             Print the revision and the update time (read)
      --diff             Print only what a revision added (follow)
      --paths            List the configuration files looked for (config)
  -h, --help             Show this help

Input:
  Text comes from the argument, from -f, or from stdin when neither is given
  and stdin is not a terminal. A text that starts with "-" must follow --, so
  it is not read as a flag: {program} add -- "-2 righe".
  add puts a newline before the new text when the board does not already end
  with one, so the addition never glues onto the last line.

Files:
  A board also carries attachments. One file per request, so a multi-file
  upload is a loop in the shell:
    {program} file upload -b lavoro ./shot.png
    {program} file list -b lavoro
    {program} file get -b lavoro <id> -o ./shot.png
    {program} file get -b lavoro --all         # every file as <board>.zip
    {program} file delete -b lavoro <id>
  file upload prints the id of the stored file (-q prints only the id), file
  list prints id, name, size and expiry, file get writes the bytes to stdout
  unless -o names a file, and file delete removes one file by id. --all packs
  the whole board into one ZIP, saved as <board>.zip unless -o says otherwise
  (- streams it), so a board with several files moves in one request; a board
  without files fails instead of writing an empty archive. An attachment
  follows the life of its board: it is deleted with the text when the board is
  cleared, and never outlives ghostboard.file_ttl_secs.

Output:
  read, "download -" and follow write the board text to stdout; confirmations
  and --stat go to stderr. --json writes one JSON object on stdout instead
  (read, config, version and the write commands); a text stream and a JSON
  object never share stdout, so download - and follow refuse --json.

Configuration:
  Keys: url, board, pin, timeout_secs; any other key is reported by config.
  Looked up next to the executable, then in
  $XDG_CONFIG_HOME/ghostboard/config.yaml (or ~/.config/ghostboard/config.yaml),
  unless --config names a file or --no-config is given.
  GO_SPIN_BOARD_URL, GO_SPIN_BOARD_PIN and GO_SPIN_BOARD_TIMEOUT_SECS override
  the file; GO_SPIN_BOARD_TOKEN is sent as a Bearer token.
  A private board unlocks itself on the first 401: the PIN is traded for a
  session token, so it travels once per command.

Private boards:
  The server protects the boards named in ghostboard.private_boards with one
  shared PIN. Store only its HMAC on the server:
    GO_SPIN_GHOSTBOARD_PIN_SECRET=... {program} pin-hash 1234
  then put the value in ghostboard.pin_hmac and export the same secret for the
  server.

Sealed boards:
  A sealed board is encrypted in the browser or here, before it leaves the
  machine: the server stores an envelope it cannot read. The key is 256 random
  bits and lives in the fragment of the link, after the #, so it never reaches
  the server, its logs or its backups.
    {program} sealed new -b nota "la password del wifi"
  prints the link and the key; hand the whole link over. Seal a later write
  with the same key:
    {program} set -b nota --key "$GB_KEY" "testo aggiornato"
    {program} read -b nota --key-file ~/.gb-nota.key
  A burn board is destroyed on the first open, so it is read with open:
    {program} open -b nota --key "$GB_KEY"
  The key comes from --key, then --key-file, then GO_SPIN_BOARD_KEY. A lost key
  means a lost board: there is no recovery, and no copy on the server.
  Sealed boards cannot carry attachments, and the web page that decrypts them
  needs HTTPS (or localhost), because WebCrypto is unavailable in a plain-HTTP
  context; this CLI has no such limit.

Exit codes:
  0 success, 1 runtime or server error, 2 misuse of the command line
  (conflicting sources, --stat with --json, invalid flag values),
  3 the URL does not expose the board API, 4 invalid board name,
  5 a sealed board was addressed without a usable key.

Examples:
  {program} add "note from the shell"
  echo "build finished" | {program} add -b lavoro
  {program} add -f note.txt -f more.txt
  {program} set ""                        # clear the default board
  {program} read --stat
  {program} read --json
  {program} upload -b lavoro ./draft.txt
  {program} upload -b lavoro --append ./more.txt
  {program} download -b lavoro -o /tmp/lavoro.txt
  {program} follow -b lavoro --diff
  {program} edit -b lavoro
  {program} file upload -b lavoro ./screenshot.png
  {program} file list -b lavoro --json
  {program} file get -b lavoro <id> -o /tmp/screenshot.png
  {program} file get -b lavoro --all -o /tmp/lavoro.zip
  {program} file delete -b lavoro <id>
  {program} read -b lavoro                # uses the pin from the configuration
  GO_SPIN_BOARD_PIN=1234 {program} follow -b lavoro
  {program} sealed new -b nota "password del wifi"
  {program} sealed new -b nota --burn=false "nota duratura"
  GB_KEY=$(...) {program} open -b nota --key "$GB_KEY"
  {program} add -b nota --key-file ~/.gb-nota.key "altra riga"
  {program} pin-hash 1234
  {program} config --paths
  {program} version --json
  {program} completion bash
`
