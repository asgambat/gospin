package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/bassista/go_spin/internal/boardclient"
)

// writeMessage writes a human-readable message to w. A failure is deliberately
// ignored: w is the diagnostic stream, so there is nowhere left to report it.
func writeMessage(w io.Writer, format string, args ...any) {
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		return
	}
}

// notify writes a confirmation unless the command runs quietly.
func notify(opts options, w io.Writer, format string, args ...any) {
	if opts.quiet {
		return
	}
	writeMessage(w, format, args...)
}

// actionReport is the machine-readable result of a mutation.
type actionReport struct {
	Board      string `json:"board"`
	Action     string `json:"action"`
	Source     string `json:"source,omitempty"`
	Path       string `json:"path,omitempty"`
	Bytes      int    `json:"bytes"`
	TotalBytes int    `json:"total_bytes,omitempty"`
	DryRun     bool   `json:"dry_run,omitempty"`
}

// report prints a mutation result: JSON on stdout with --json, otherwise the
// human confirmation on stderr (suppressed by --quiet). It returns the exit
// code of the command.
func report(opts options, stdout, stderr io.Writer, result actionReport, human string, args ...any) int {
	if !opts.json {
		notify(opts, stderr, human, args...)
		return exitOK
	}

	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		writeMessage(stderr, "%s: writing the result: %v\n", opts.program, err)
		return exitError
	}
	return exitOK
}

// writeBoardText writes the board text verbatim. The missing final newline is
// added only on a terminal, so an interactive read does not glue the shell
// prompt to the text while pipes and files still get the exact bytes.
func writeBoardText(stdout io.Writer, text string) error {
	if text == "" {
		return nil
	}

	out := text
	if isTerminal(stdout) && !strings.HasSuffix(text, "\n") {
		out = text + "\n"
	}

	if _, err := io.WriteString(stdout, out); err != nil {
		return fmt.Errorf("writing board text: %w", err)
	}
	return nil
}

// writeBoardSnapshot prints one revision of a board in follow mode. Each event
// carries the whole text, so a revision is always printed in full; the missing
// final newline is added so consecutive revisions never glue together.
func writeBoardSnapshot(stdout io.Writer, text string) {
	writeBoardChunk(stdout, text)
}

// writeBoardDelta prints only what a revision added to the previous one. The
// first revision is printed in full; a revision that is not an extension of the
// previous one (an edit or a replacement) is printed in full too, because there
// is no meaningful delta to show.
func writeBoardDelta(stdout io.Writer, previous, current string, first bool) {
	// An empty board has nothing to show, and a blank line would only be noise
	// at the start of the stream.
	if first && current == "" {
		return
	}
	if first || !strings.HasPrefix(current, previous) {
		writeBoardSnapshot(stdout, current)
		return
	}

	// The separator add inserts belongs to the existing text, not to the new
	// lines, so it is not reprinted as an empty line.
	delta := strings.TrimPrefix(current[len(previous):], "\n")
	if delta == "" {
		return
	}
	writeBoardChunk(stdout, delta)
}

// writeBoardChunk writes a text followed by a newline when it lacks one.
func writeBoardChunk(stdout io.Writer, text string) {
	out := text
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}

	// A failure is deliberately ignored: stdout is the stream being watched.
	if _, err := io.WriteString(stdout, out); err != nil {
		return
	}
}

// isTerminal reports whether a stream is an interactive terminal. It accepts
// readers and writers because both stdin and stdout are probed.
func isTerminal(stream any) bool {
	file, ok := stream.(*os.File)
	if !ok {
		return false
	}

	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// statLine formats the revision summary printed by read --stat. A board that
// was never written has no update time, and inventing one would be a lie.
func statLine(board boardclient.Board) string {
	if board.UpdatedAt == nil {
		return fmt.Sprintf("board %q: revision %d", board.Name, board.Revision)
	}
	return fmt.Sprintf("board %q: revision %d, updated %s", board.Name, board.Revision, board.UpdatedAt.UTC().Format(time.RFC3339))
}
