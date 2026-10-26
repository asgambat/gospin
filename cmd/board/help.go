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
  config            Print the configuration in use (no request is made)
  completion <sh>   Print a completion script (bash, zsh or fish)
  version           Print the build metadata

Flags:
  -b, --board <name>     Board to use (default: from configuration, else "default")
  -u, --url <url>        GoSpin server URL (default: from configuration, else
                         http://localhost:8084)
      --timeout <dur>    Request timeout, e.g. 30s or 1m
      --config <path>    Configuration file to use
      --no-config        Ignore the configuration files
      --retry <n>        Retry a transient failure (429/502/503/504) n times
      --header <header>  Extra HTTP header, "Name: Value", repeatable
  -q, --quiet            Suppress the confirmations on stderr
      --json             Write a machine-readable result on stdout
  -f <file>              File to read the text from, repeatable (add, set)
      --dry-run          Show what would change without writing (add, set, upload)
  -o <file>              Output file (download)
      --force            Overwrite an existing file (download)
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

Output:
  read, "download -" and follow write the board text to stdout; confirmations
  and --stat go to stderr. --json writes one JSON object on stdout instead
  (read, config, version and the write commands); a text stream and a JSON
  object never share stdout, so download - and follow refuse --json.

Configuration:
  Keys: url, board, timeout_secs; any other key is reported by config.
  Looked up next to the executable, then in
  $XDG_CONFIG_HOME/ghostboard/config.yaml (or ~/.config/ghostboard/config.yaml),
  unless --config names a file or --no-config is given.
  GO_SPIN_BOARD_URL and GO_SPIN_BOARD_TIMEOUT_SECS override the file;
  GO_SPIN_BOARD_TOKEN is sent as a Bearer token.

Exit codes:
  0 success, 1 runtime or server error, 2 misuse of the command line
  (conflicting sources, --stat with --json, invalid flag values),
  3 the URL does not expose the board API, 4 invalid board name.

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
  {program} config --paths
  {program} version --json
  {program} completion bash
`
