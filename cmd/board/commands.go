package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/asgambat/gospin/internal/boardclient"
	"github.com/asgambat/gospin/internal/ghostboard"
	"github.com/asgambat/gospin/internal/sealedcrypto"
)

// downloadPerm is the permission of a board saved with download.
const downloadPerm = 0o644

// execAdd appends text to the board.
func execAdd(ctx context.Context, client *boardclient.Client, opts options, positionals []string, stdin io.Reader, stdout, stderr io.Writer) int {
	text, err := readText(positionals, opts.files, stdin)
	if err != nil {
		return fail(opts.program, stderr, err)
	}
	if err := validateText(text, "the text to add"); err != nil {
		return fail(opts.program, stderr, err)
	}

	if opts.dryRun {
		current, err := currentText(ctx, client, opts)
		if err != nil {
			return fail(opts.program, stderr, err)
		}
		updated := boardclient.AppendText(current, text)
		return report(opts, stdout, stderr, actionReport{
			Board:      opts.board,
			Action:     "append",
			Bytes:      len(text),
			TotalBytes: len(updated),
			DryRun:     true,
		}, "board %q: would add %d byte(s), %d byte(s) in total (dry run)\n", opts.board, len(text), len(updated))
	}

	if hasSealedKey(opts) {
		key, err := resolveSealedKey(opts)
		if err != nil {
			return fail(opts.program, stderr, err)
		}
		current, err := currentText(ctx, client, opts)
		if err != nil {
			return fail(opts.program, stderr, err)
		}
		updated := boardclient.AppendText(current, text)
		if err := client.SealedWrite(ctx, opts.board, updated, key, false); err != nil {
			return fail(opts.program, stderr, err)
		}
		return report(opts, stdout, stderr, actionReport{
			Board:      opts.board,
			Action:     "append",
			Bytes:      len(text),
			TotalBytes: len(updated),
		}, "board %q updated: %d byte(s) added to the sealed board, %d byte(s) in total\n", opts.board, len(text), len(updated))
	}

	updated, err := client.Append(ctx, opts.board, text)
	if err != nil {
		return fail(opts.program, stderr, err)
	}

	return report(opts, stdout, stderr, actionReport{
		Board:      opts.board,
		Action:     "append",
		Bytes:      len(text),
		TotalBytes: len(updated),
	}, "board %q updated: %d byte(s) added, %d byte(s) in total\n", opts.board, len(text), len(updated))
}

// currentText reads the text a command works from, decrypting it when a sealed
// key is configured.
func currentText(ctx context.Context, client *boardclient.Client, opts options) (string, error) {
	if !hasSealedKey(opts) {
		return client.Get(ctx, opts.board)
	}
	key, err := resolveSealedKey(opts)
	if err != nil {
		return "", err
	}
	state, err := readSealedState(ctx, client, opts.board, key)
	if err != nil {
		return "", err
	}
	return state.Text, nil
}

// execSet replaces the whole text of the board.
func execSet(ctx context.Context, client *boardclient.Client, opts options, positionals []string, stdin io.Reader, stdout, stderr io.Writer) int {
	text, err := readText(positionals, opts.files, stdin)
	if err != nil {
		return fail(opts.program, stderr, err)
	}
	if err := validateText(text, "the text to write"); err != nil {
		return fail(opts.program, stderr, err)
	}

	result := actionReport{Board: opts.board, Action: "set", Bytes: len(text), DryRun: opts.dryRun}
	if opts.dryRun {
		return report(opts, stdout, stderr, result,
			"board %q: would be replaced with %d byte(s) (dry run)\n", opts.board, len(text))
	}

	if hasSealedKey(opts) {
		key, err := resolveSealedKey(opts)
		if err != nil {
			return fail(opts.program, stderr, err)
		}
		if err := client.SealedWrite(ctx, opts.board, text, key, false); err != nil {
			return fail(opts.program, stderr, err)
		}
		return report(opts, stdout, stderr, result, "board %q replaced with a sealed envelope: %d byte(s)\n", opts.board, len(text))
	}

	if err := client.Set(ctx, opts.board, text); err != nil {
		return fail(opts.program, stderr, err)
	}

	return report(opts, stdout, stderr, result, "board %q replaced: %d byte(s)\n", opts.board, len(text))
}

// execRead prints the board text on stdout.
func execRead(ctx context.Context, client *boardclient.Client, opts options, positionals []string, stdout, stderr io.Writer) int {
	if len(positionals) > 0 {
		return usageError(opts.program, stderr, "read", "unexpected argument %q", positionals[0])
	}
	if opts.json && opts.stat {
		return usageError(opts.program, stderr, "read", "--json already carries the revision and the update time: drop --stat")
	}

	if opts.json || opts.stat {
		board, err := sealedState(ctx, client, opts)
		if err != nil {
			return fail(opts.program, stderr, err)
		}
		if board.Name == "" {
			board.Name = opts.board
		}

		if opts.json {
			if err := json.NewEncoder(stdout).Encode(board); err != nil {
				return fail(opts.program, stderr, fmt.Errorf("writing the board state: %w", err))
			}
			return exitOK
		}

		writeMessage(stderr, "%s\n", statLine(board))
		if err := writeBoardText(stdout, board.Text); err != nil {
			return fail(opts.program, stderr, err)
		}
		return exitOK
	}

	text, err := currentText(ctx, client, opts)
	if err != nil {
		return fail(opts.program, stderr, err)
	}
	if err := writeBoardText(stdout, text); err != nil {
		return fail(opts.program, stderr, err)
	}
	return exitOK
}

// execUpload loads a board from a file, replacing its text or appending to it.
func execUpload(ctx context.Context, client *boardclient.Client, opts options, positionals []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(positionals) != 1 {
		return usageError(opts.program, stderr, "upload", "needs exactly one file (use - to read stdin)")
	}

	source := positionals[0]
	text, err := readSource(source, stdin)
	if err != nil {
		return fail(opts.program, stderr, err)
	}
	if err := validateText(text, source); err != nil {
		return fail(opts.program, stderr, err)
	}

	if opts.appendMode {
		if opts.dryRun {
			current, err := client.Get(ctx, opts.board)
			if err != nil {
				return fail(opts.program, stderr, err)
			}
			updated := boardclient.AppendText(current, text)
			return report(opts, stdout, stderr, actionReport{
				Board:      opts.board,
				Action:     "append",
				Source:     source,
				Bytes:      len(text),
				TotalBytes: len(updated),
				DryRun:     true,
			}, "board %q: would add %d byte(s) from %s, %d byte(s) in total (dry run)\n", opts.board, len(text), source, len(updated))
		}

		updated, err := client.Append(ctx, opts.board, text)
		if err != nil {
			return fail(opts.program, stderr, err)
		}
		return report(opts, stdout, stderr, actionReport{
			Board:      opts.board,
			Action:     "append",
			Source:     source,
			Bytes:      len(text),
			TotalBytes: len(updated),
		}, "board %q updated: %d byte(s) added from %s, %d byte(s) in total\n", opts.board, len(text), source, len(updated))
	}

	result := actionReport{Board: opts.board, Action: "set", Source: source, Bytes: len(text), DryRun: opts.dryRun}
	if opts.dryRun {
		return report(opts, stdout, stderr, result,
			"board %q: would be loaded from %s with %d byte(s) (dry run)\n", opts.board, source, len(text))
	}

	if err := client.Set(ctx, opts.board, text); err != nil {
		return fail(opts.program, stderr, err)
	}

	return report(opts, stdout, stderr, result, "board %q loaded from %s: %d byte(s)\n", opts.board, source, len(text))
}

// execDownload saves the board text to a file, `<board>.txt` by default.
func execDownload(ctx context.Context, client *boardclient.Client, opts options, positionals []string, stdout, stderr io.Writer) int {
	if opts.output != "" && len(positionals) > 0 {
		return usageError(opts.program, stderr, "download", "give the output either with -o or as an argument, not both")
	}
	if len(positionals) > 1 {
		return usageError(opts.program, stderr, "download", "takes at most one output file")
	}

	target := opts.output
	if target == "" && len(positionals) == 1 {
		target = positionals[0]
	}
	if target == "" {
		target = opts.board + ".txt"
	}

	text, err := client.Get(ctx, opts.board)
	if err != nil {
		return fail(opts.program, stderr, err)
	}

	if target == "-" {
		// The board text and a JSON result would both land on stdout, so the
		// two outputs cannot be asked for together: read --json carries the text
		// and the revision in one object.
		if opts.json {
			return usageError(opts.program, stderr, "download", "the board text and the JSON result would share stdout: drop --json or write to a file")
		}
		if err := writeBoardText(stdout, text); err != nil {
			return fail(opts.program, stderr, err)
		}
		return exitOK
	}

	if !opts.force {
		if _, statErr := os.Stat(target); statErr == nil {
			return fail(opts.program, stderr, fmt.Errorf("%s already exists: use --force to overwrite it", target))
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return fail(opts.program, stderr, fmt.Errorf("checking %s: %w", target, statErr))
		}
	}

	if err := os.WriteFile(target, []byte(text), downloadPerm); err != nil {
		return fail(opts.program, stderr, fmt.Errorf("writing %s: %w", target, err))
	}

	return report(opts, stdout, stderr, actionReport{
		Board:  opts.board,
		Action: "download",
		Path:   target,
		Bytes:  len(text),
	}, "board %q saved to %s (%d byte(s))\n", opts.board, target, len(text))
}

// execFollow prints the board and then every change until the user stops it.
func execFollow(ctx context.Context, client *boardclient.Client, opts options, positionals []string, stdout, stderr io.Writer) int {
	if len(positionals) > 0 {
		return usageError(opts.program, stderr, "follow", "unexpected argument %q", positionals[0])
	}
	if opts.json {
		return usageError(opts.program, stderr, "follow", "streams the board text: --json is not supported, use read --json for one revision")
	}

	// The stream stays open until the server closes it, so Ctrl-C has to cancel
	// the request instead of killing the process mid-write.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cancelled := watchSignals(cancel)
	defer cancelled()

	// A sealed board streams envelopes: each snapshot is decrypted here, so the
	// caller of follow sees the same text the page does.
	var key sealedcrypto.Key
	if hasSealedKey(opts) {
		var err error
		if key, err = resolveSealedKey(opts); err != nil {
			return fail(opts.program, stderr, err)
		}
	}

	// The announcement waits for the first revision: a wrong URL must print the
	// error alone, without claiming the stream was opened.
	announced := false
	previous := ""
	var decryptErr error

	if err := client.Follow(ctx, opts.board, func(board boardclient.Board) {
		if board.Sealed() && board.Text != "" {
			plaintext, err := sealedcrypto.Open(key, opts.board, board.Text)
			if err != nil {
				decryptErr = &boardclient.SealedKeyError{Board: opts.board}
				cancel()
				return
			}
			board.Text = string(plaintext)
		}
		if !announced {
			announced = true
			notify(opts, stderr, "following board %q on %s (press Ctrl-C to stop)\n", opts.board, opts.url)
		}

		if opts.diff {
			writeBoardDelta(stdout, previous, board.Text, previous == "")
			previous = board.Text
			return
		}
		writeBoardSnapshot(stdout, board.Text)
	}); err != nil {
		return fail(opts.program, stderr, err)
	}
	if decryptErr != nil {
		return fail(opts.program, stderr, decryptErr)
	}

	return exitOK
}

// execEdit opens the board in $VISUAL/$EDITOR and writes it back when it
// changed. The editor runs attached to the terminal, so it can be interactive.
func execEdit(ctx context.Context, client *boardclient.Client, opts options, positionals []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(positionals) > 0 {
		return usageError(opts.program, stderr, "edit", "unexpected argument %q", positionals[0])
	}

	editor := editorCommand()
	if len(editor) == 0 {
		return fail(opts.program, stderr, errors.New("no editor found: set VISUAL or EDITOR"))
	}

	current, err := client.Get(ctx, opts.board)
	if err != nil {
		return fail(opts.program, stderr, err)
	}

	path, err := writeTempText(current)
	if err != nil {
		return fail(opts.program, stderr, err)
	}
	defer removeFile(path)

	command := exec.Command(editor[0], append(editor[1:], path)...)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fail(opts.program, stderr, fmt.Errorf("running the editor: %w", err))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fail(opts.program, stderr, fmt.Errorf("reading the edited text: %w", err))
	}

	updated := string(data)
	if updated == current {
		return report(opts, stdout, stderr, actionReport{
			Board:  opts.board,
			Action: "unchanged",
			Bytes:  len(updated),
		}, "board %q unchanged\n", opts.board)
	}
	if err := validateText(updated, "the edited text"); err != nil {
		return fail(opts.program, stderr, err)
	}

	if err := client.Set(ctx, opts.board, updated); err != nil {
		return fail(opts.program, stderr, err)
	}

	return report(opts, stdout, stderr, actionReport{
		Board:  opts.board,
		Action: "set",
		Bytes:  len(updated),
	}, "board %q replaced: %d byte(s)\n", opts.board, len(updated))
}

// configPathReport is one configuration file the CLI looks for.
type configPathReport struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

// configReport is the machine-readable form of the config command. The PIN
// itself is never printed, only whether one is configured.
type configReport struct {
	ConfigFile     string             `json:"config_file"`
	ServerURL      string             `json:"server_url"`
	Board          string             `json:"board"`
	Executable     string             `json:"executable"`
	Paths          []configPathReport `json:"paths,omitempty"`
	Environment    []string           `json:"environment,omitempty"`
	Warnings       []string           `json:"warnings,omitempty"`
	TimeoutSeconds int                `json:"timeout_secs"`
	PINSet         bool               `json:"pin_set"`
}

// execConfig prints the configuration the other commands would use. It never
// contacts the server, so a wrong URL, a configuration file that is not being
// found or a configuration that requires a flag are all visible before a
// request is made.
func execConfig(opts options, cfg boardclient.Config, candidates []string, positionals []string, stdout, stderr io.Writer) int {
	if len(positionals) > 0 {
		return usageError(opts.program, stderr, "config", "unexpected argument %q", positionals[0])
	}

	source := cfg.FilePath
	if source == "" {
		source = "none found, using the built-in defaults"
	}
	if opts.noConfig {
		source = "ignored (--no-config), using the environment and the flags"
	}

	executable := "unknown"
	if path := executablePath(); path != "" {
		executable = path
	}

	var paths []configPathReport
	if opts.paths {
		paths = make([]configPathReport, 0, len(candidates))
		for _, candidate := range candidates {
			_, err := os.Stat(candidate)
			paths = append(paths, configPathReport{Path: candidate, Exists: err == nil})
		}
	}

	if opts.json {
		report := configReport{
			ConfigFile:     source,
			ServerURL:      opts.url,
			Board:          opts.board,
			Executable:     executable,
			TimeoutSeconds: int(opts.timeout.Seconds()),
			Paths:          paths,
			Environment:    cfg.EnvOverrides,
			Warnings:       cfg.Warnings,
			PINSet:         opts.pin != "",
		}
		if err := json.NewEncoder(stdout).Encode(report); err != nil {
			return fail(opts.program, stderr, fmt.Errorf("writing the configuration: %w", err))
		}
	} else {
		writeMessage(stdout, "config file:  %s\n", source)
		writeMessage(stdout, "server url:   %s\n", opts.url)
		writeMessage(stdout, "board:        %s\n", opts.board)
		writeMessage(stdout, "timeout:      %s\n", opts.timeout)
		writeMessage(stdout, "pin:          %s\n", pinState(opts.pin))
		writeMessage(stdout, "executable:   %s\n", executable)
		if len(cfg.EnvOverrides) > 0 {
			writeMessage(stdout, "environment:  %s (overrides the file)\n", strings.Join(cfg.EnvOverrides, ", "))
		}
		for _, warning := range cfg.Warnings {
			writeMessage(stdout, "warning:      %s\n", warning)
		}
		if opts.paths {
			writeMessage(stdout, "\nlooked up:\n")
			for _, path := range paths {
				state := "missing"
				if path.Exists {
					state = "found"
				}
				writeMessage(stdout, "  %-60s %s\n", path.Path, state)
			}
		}
	}

	if err := (boardclient.Config{URL: opts.url, Board: opts.board, PIN: opts.pin, Timeout: opts.timeout}).Validate(); err != nil {
		return fail(opts.program, stderr, err)
	}

	return exitOK
}

// pinState describes the PIN without printing it, so a config report can be
// pasted into an issue without leaking the secret.
func pinState(pin string) string {
	if pin == "" {
		return "not set (public boards only)"
	}
	return "set"
}

// execPinHash prints the value to put in ghostboard.pin_hmac. The PIN comes
// from the argument or from stdin, the secret from
// GO_SPIN_GHOSTBOARD_PIN_SECRET, so neither has to appear in a configuration
// file.
func execPinHash(opts options, positionals []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(positionals) > 1 {
		return usageError(opts.program, stderr, "pin-hash", "unexpected argument %q", positionals[1])
	}

	pin, err := readPIN(positionals, stdin)
	if err != nil {
		return fail(opts.program, stderr, err)
	}

	secret := strings.TrimSpace(os.Getenv("GO_SPIN_GHOSTBOARD_PIN_SECRET"))
	if secret == "" {
		return fail(opts.program, stderr, errors.New("GO_SPIN_GHOSTBOARD_PIN_SECRET must hold the secret of the GoSpin server"))
	}

	digest, err := ghostboard.PINDigest(secret, pin)
	if err != nil {
		return fail(opts.program, stderr, err)
	}

	if opts.json {
		if err := json.NewEncoder(stdout).Encode(struct {
			PINHMAC string `json:"pin_hmac"`
		}{PINHMAC: digest}); err != nil {
			return fail(opts.program, stderr, fmt.Errorf("writing the pin digest: %w", err))
		}
		return exitOK
	}

	writeMessage(stdout, "%s\n", digest)
	return exitOK
}

// readPIN resolves the PIN of pin-hash from the argument or from stdin.
func readPIN(positionals []string, stdin io.Reader) (string, error) {
	if len(positionals) == 1 {
		return strings.TrimSpace(positionals[0]), nil
	}

	data, err := io.ReadAll(io.LimitReader(stdin, 1024))
	if err != nil {
		return "", fmt.Errorf("reading the pin from stdin: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// readText resolves the text of add and set from an argument, from -f (possibly
// repeated) or from stdin.
func readText(positionals, files []string, stdin io.Reader) (string, error) {
	if len(files) > 0 && len(positionals) > 0 {
		return "", misuse(errors.New("give the text either as an argument or with -f, not both"))
	}

	if len(files) > 0 {
		// Several -f values are joined with the same rule as add itself, so two
		// files without a trailing newline never glue their last and first line.
		var combined string
		for _, file := range files {
			text, err := readSource(file, stdin)
			if err != nil {
				return "", err
			}
			combined = boardclient.AppendText(combined, text)
		}
		return combined, nil
	}

	if len(positionals) > 0 {
		return strings.Join(positionals, " "), nil
	}
	if isTerminal(stdin) {
		return "", misuse(errors.New("no text given: pass it as an argument, with -f or on stdin"))
	}
	return readSource("-", stdin)
}

// readSource reads a file, or stdin when name is "-".
func readSource(name string, stdin io.Reader) (string, error) {
	if name == "-" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		return string(data), nil
	}

	data, err := os.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", name, err)
	}
	return string(data), nil
}

// validateText rejects a text a board cannot hold, naming its source so the
// message points at the file to fix. The server rejects invalid UTF-8 too; the
// local check avoids a request and gives a message about the local file.
func validateText(text, source string) error {
	if utf8.ValidString(text) {
		return nil
	}
	return fmt.Errorf("%s is not valid UTF-8: boards hold text only, save it as UTF-8", source)
}

// editorCommand returns the editor to run, honouring VISUAL before EDITOR. The
// value may carry arguments, so it is split on whitespace.
func editorCommand() []string {
	for _, name := range []string{"VISUAL", "EDITOR"} {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			continue
		}
		if parts := strings.Fields(value); len(parts) > 0 {
			return parts
		}
	}
	return nil
}

// writeTempText stores the board text in a temporary file for the editor.
func writeTempText(text string) (string, error) {
	file, err := os.CreateTemp("", "gb-cli-*.txt")
	if err != nil {
		return "", fmt.Errorf("creating the temporary file: %w", err)
	}

	path := file.Name()
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("closing the temporary file: %w", err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return "", fmt.Errorf("writing the temporary file: %w", err)
	}
	return path, nil
}

// removeFile deletes a temporary file. A failure is ignored: the command has
// already produced its result and the file lives in the temporary directory.
func removeFile(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return
	}
}

// watchSignals cancels the stream on Ctrl-C. The returned function stops
// watching.
func watchSignals(cancel context.CancelFunc) func() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})

	go func() {
		select {
		case <-signals:
			cancel()
		case <-done:
		}
	}()

	return func() {
		signal.Stop(signals)
		close(done)
	}
}
