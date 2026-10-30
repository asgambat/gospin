package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/asgambat/gospin/internal/boardclient"
	"github.com/asgambat/gospin/internal/sealedcrypto"
)

const (
	// sealedKeyEnv carries a sealed board key, so a script does not have to
	// repeat a secret on the command line where ps and the shell history keep it.
	sealedKeyEnv = "GO_SPIN_BOARD_KEY"

	// sealedKeySourceFlags is the precedence of the key sources, documented in
	// the help and in the error of a missing key.
	sealedKeySourceFlags = "--key, --key-file or GO_SPIN_BOARD_KEY"
)

// ErrSealedKeyMissing marks a sealed command without a key.
var ErrSealedKeyMissing = errors.New("this board is sealed: pass its key with " + sealedKeySourceFlags)

// ErrSealedBurnRead marks a plain read of a burn board: the note has to be
// consumed with open, because reading it would destroy it either way.
var ErrSealedBurnRead = errors.New("this board burns after one read: use open to consume it")

// sealedReport is the machine-readable result of sealed new.
type sealedReport struct {
	Board string `json:"board"`
	Mode  string `json:"mode"`
	Link  string `json:"link"`
	Key   string `json:"key"`
	Burn  bool   `json:"burn"`
	Bytes int    `json:"bytes"`
}

// openReport is the machine-readable result of open. The text is the decrypted
// one: the envelope is gone from the server by the time the report is printed.
type openReport struct {
	Board string `json:"board"`
	Mode  string `json:"mode"`
	Text  string `json:"text"`
	Burn  bool   `json:"burn"`
}

// resolveSealedKey returns the key from --key, --key-file or GO_SPIN_BOARD_KEY,
// in that order. The error names every source, so a mistyped environment
// variable does not look like a broken board.
func resolveSealedKey(opts options) (sealedcrypto.Key, error) {
	if opts.key != "" {
		key, err := sealedcrypto.ParseKey(opts.key)
		if err != nil {
			return sealedcrypto.Key{}, fmt.Errorf("--key is not a usable key (43 base64url characters, 64 hex characters, or a whole sealed link): %w", err)
		}
		return key, nil
	}

	if opts.keyFile != "" {
		data, err := os.ReadFile(opts.keyFile)
		if err != nil {
			return sealedcrypto.Key{}, fmt.Errorf("reading the key file: %w", err)
		}
		key, err := sealedcrypto.ParseKey(string(data))
		if err != nil {
			return sealedcrypto.Key{}, fmt.Errorf("%s does not carry a usable key: %w", opts.keyFile, err)
		}
		return key, nil
	}

	if value := strings.TrimSpace(os.Getenv(sealedKeyEnv)); value != "" {
		key, err := sealedcrypto.ParseKey(value)
		if err != nil {
			return sealedcrypto.Key{}, fmt.Errorf("%s does not carry a usable key: %w", sealedKeyEnv, err)
		}
		return key, nil
	}

	return sealedcrypto.Key{}, ErrSealedKeyMissing
}

// hasSealedKey reports whether the command was given a key by any source, so the
// read and write commands know whether to take the sealed path.
func hasSealedKey(opts options) bool {
	return opts.key != "" || opts.keyFile != "" || strings.TrimSpace(os.Getenv(sealedKeyEnv)) != ""
}

// sealedLink builds the page URL that carries the key in its fragment. The key
// never travels to the server: a fragment is not part of the request.
func sealedLink(serverURL, board string, key sealedcrypto.Key) string {
	return strings.TrimRight(serverURL, "/") + "/ui/sealed/" + url.PathEscape(board) + "#k=" + key.String()
}

// execSealedNew creates a sealed board and prints the link that holds its key.
func execSealedNew(ctx context.Context, client *boardclient.Client, opts options, positionals []string, stdin io.Reader, stdout, stderr io.Writer) int {
	text, err := readText(positionals, opts.files, stdin)
	if err != nil {
		return fail(opts.program, stderr, err)
	}
	if err := validateText(text, "the text to seal"); err != nil {
		return fail(opts.program, stderr, err)
	}
	if text == "" {
		return usageError(opts.program, stderr, "sealed new", "no text to seal: pass it as an argument, with -f or on stdin")
	}

	key, err := resolveSealedKey(opts)
	generated := false
	if errors.Is(err, ErrSealedKeyMissing) {
		if key, err = sealedcrypto.NewKey(); err != nil {
			return fail(opts.program, stderr, err)
		}
		generated = true
	} else if err != nil {
		return fail(opts.program, stderr, err)
	}

	if err := client.SealedWrite(ctx, opts.board, text, key, opts.burn); err != nil {
		return fail(opts.program, stderr, err)
	}

	link := sealedLink(opts.url, opts.board, key)

	if opts.json {
		if err := json.NewEncoder(stdout).Encode(sealedReport{
			Board: opts.board,
			Mode:  "sealed",
			Link:  link,
			Key:   key.String(),
			Burn:  opts.burn,
			Bytes: len(text),
		}); err != nil {
			return fail(opts.program, stderr, fmt.Errorf("writing the sealed board: %w", err))
		}
		return exitOK
	}

	// The link is the product of this command, so it goes to stdout even when
	// --quiet silences the confirmation.
	writeMessage(stdout, "%s\n", link)
	if !opts.quiet {
		writeMessage(stderr, "board %q sealed%s (%d byte(s))\n", opts.board, burnLabel(opts.burn), len(text))
		writeMessage(stderr, "key:  %s%s\n", key.String(), generatedLabel(generated))
		writeMessage(stderr, "The key exists only here and in the link: GoSpin stores ciphertext and cannot recover it.\n")
	}
	return exitOK
}

// execOpen consumes a burn board once and prints the decrypted text.
func execOpen(ctx context.Context, client *boardclient.Client, opts options, positionals []string, stdout, stderr io.Writer) int {
	if len(positionals) > 0 {
		return usageError(opts.program, stderr, "open", "unexpected argument %q", positionals[0])
	}
	if opts.dryRun {
		return usageError(opts.program, stderr, "open", "--dry-run cannot check a burn board: opening it destroys it")
	}

	key, err := resolveSealedKey(opts)
	if errors.Is(err, ErrSealedKeyMissing) {
		writeMessage(stderr, "%s: %v\n", opts.program, err)
		return exitSealedKey
	}
	if err != nil {
		return fail(opts.program, stderr, err)
	}

	text, err := client.SealedOpen(ctx, opts.board, key)
	if err != nil {
		return fail(opts.program, stderr, err)
	}

	if opts.json {
		if err := json.NewEncoder(stdout).Encode(openReport{
			Board: opts.board,
			Mode:  "sealed",
			Burn:  true,
			Text:  text,
		}); err != nil {
			return fail(opts.program, stderr, fmt.Errorf("writing the opened board: %w", err))
		}
		return exitOK
	}

	notify(opts, stderr, "board %q opened and destroyed: it no longer exists on the server\n", opts.board)
	if err := writeBoardText(stdout, text); err != nil {
		return fail(opts.program, stderr, err)
	}
	return exitOK
}

// execSealed dispatches the sealed subcommands.
func execSealed(ctx context.Context, client *boardclient.Client, opts options, positionals []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(positionals) == 0 {
		return usageError(opts.program, stderr, "sealed", "missing action: use new")
	}

	action := positionals[0]
	rest := positionals[1:]
	switch action {
	case "new":
		return execSealedNew(ctx, client, opts, rest, stdin, stdout, stderr)
	default:
		return usageError(opts.program, stderr, "sealed", "unknown action %q: use new", action)
	}
}

// sealedState reads the state of the board, decrypting it when a key is set.
func sealedState(ctx context.Context, client *boardclient.Client, opts options) (boardclient.Board, error) {
	if !hasSealedKey(opts) {
		return client.GetBoard(ctx, opts.board)
	}

	key, err := resolveSealedKey(opts)
	if err != nil {
		return boardclient.Board{}, err
	}
	return readSealedState(ctx, client, opts.board, key)
}

// readSealedState reads a board and decrypts it when it is sealed. A burn board is
// refused: the only way to read it is open, which destroys it.
func readSealedState(ctx context.Context, client *boardclient.Client, board string, key sealedcrypto.Key) (boardclient.Board, error) {
	state, err := client.GetBoard(ctx, board)
	if err != nil {
		return boardclient.Board{}, err
	}
	if !state.Sealed() || state.Text == "" {
		return state, nil
	}
	if state.Burn {
		return boardclient.Board{}, ErrSealedBurnRead
	}

	plaintext, err := sealedcrypto.Open(key, board, state.Text)
	if err != nil {
		if errors.Is(err, sealedcrypto.ErrDecrypt) {
			return boardclient.Board{}, &boardclient.SealedKeyError{Board: board}
		}
		return boardclient.Board{}, fmt.Errorf("board %q: %w", board, err)
	}
	state.Text = string(plaintext)
	return state, nil
}

// burnLabel describes the burn flag for a confirmation.
func burnLabel(burn bool) string {
	if burn {
		return " to burn after the first open"
	}
	return ""
}

// generatedLabel says whether the key was minted by this command.
func generatedLabel(generated bool) string {
	if generated {
		return " (generated now: this is the only copy)"
	}
	return ""
}
