package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/asgambat/gospin/internal/boardclient"
)

// execFile runs the `file` command: upload, list, get and delete of the
// attachments of a board. The board text commands keep their names
// (upload/set/read), so the two surfaces never shadow each other.
func execFile(ctx context.Context, client *boardclient.Client, opts options, positionals []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(positionals) == 0 {
		return usageError(opts.program, stderr, "file", "needs an action: upload, list, get or delete")
	}

	action, rest := positionals[0], positionals[1:]
	switch action {
	case "upload":
		return execFileUpload(ctx, client, opts, rest, stdin, stdout, stderr)
	case "list":
		return execFileList(ctx, client, opts, rest, stdout, stderr)
	case "get":
		return execFileGet(ctx, client, opts, rest, stdout, stderr)
	case "delete":
		return execFileDelete(ctx, client, opts, rest, stdout, stderr)
	default:
		return usageError(opts.program, stderr, "file", "unknown action %q: use upload, list, get or delete", action)
	}
}

// execFileUpload sends one file to the board and prints what the server stored.
func execFileUpload(ctx context.Context, client *boardclient.Client, opts options, positionals []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(positionals) != 1 {
		return usageError(opts.program, stderr, "file upload", "needs exactly one file (use - to read stdin)")
	}

	source := positionals[0]
	name := filepath.Base(source)

	var (
		content io.Reader
		file    *os.File
	)
	if source == "-" {
		content = stdin
		name = "stdin"
	} else {
		opened, err := os.Open(source)
		if err != nil {
			return fail(opts.program, stderr, fmt.Errorf("reading %s: %w", source, err))
		}
		file = opened
		content = opened
	}
	if file != nil {
		defer func() {
			if err := file.Close(); err != nil {
				writeMessage(stderr, "%s: closing %s: %v\n", opts.program, source, err)
			}
		}()
	}

	stored, err := client.UploadFile(ctx, opts.board, name, content)
	if err != nil {
		return fail(opts.program, stderr, err)
	}

	if opts.quiet {
		writeMessage(stdout, "%s\n", stored.ID)
		return exitOK
	}
	if opts.json {
		if err := json.NewEncoder(stdout).Encode(stored); err != nil {
			return fail(opts.program, stderr, fmt.Errorf("writing the upload result: %w", err))
		}
		return exitOK
	}

	writeMessage(stdout, "board %q: %s uploaded as %s (%s)\n", opts.board, stored.Name, stored.ID, byteSize(stored.Size))
	return exitOK
}

// execFileList prints the attachments of the board.
func execFileList(ctx context.Context, client *boardclient.Client, opts options, positionals []string, stdout, stderr io.Writer) int {
	if len(positionals) > 0 {
		return usageError(opts.program, stderr, "file list", "unexpected argument %q", positionals[0])
	}

	files, err := client.ListFiles(ctx, opts.board)
	if err != nil {
		return fail(opts.program, stderr, err)
	}

	if opts.json {
		if err := json.NewEncoder(stdout).Encode(files); err != nil {
			return fail(opts.program, stderr, fmt.Errorf("writing the file list: %w", err))
		}
		return exitOK
	}

	if len(files) == 0 {
		notify(opts, stderr, "board %q has no files\n", opts.board)
		return exitOK
	}

	writer := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	writeMessage(writer, "ID\tNAME\tSIZE\tEXPIRES\n")
	for _, file := range files {
		expires := ""
		if file.ExpiresAt != nil {
			expires = file.ExpiresAt.Local().Format(time.RFC3339)
		}
		writeMessage(writer, "%s\t%s\t%s\t%s\n", file.ID, file.Name, byteSize(file.Size), expires)
	}
	if err := writer.Flush(); err != nil {
		return fail(opts.program, stderr, fmt.Errorf("writing the file list: %w", err))
	}
	return exitOK
}

// execFileGet downloads one attachment to a file, or to stdout when no output
// is given.
func execFileGet(ctx context.Context, client *boardclient.Client, opts options, positionals []string, stdout, stderr io.Writer) int {
	if opts.output != "" && len(positionals) > 1 {
		return usageError(opts.program, stderr, "file get", "give the id as the only argument when -o names the output")
	}
	if len(positionals) != 1 {
		return usageError(opts.program, stderr, "file get", "needs exactly one file id")
	}
	id := strings.TrimSpace(positionals[0])

	target := opts.output
	if target == "" {
		target = "-"
	}
	if target == "-" && opts.json {
		return usageError(opts.program, stderr, "file get", "the bytes and a JSON result would share stdout: drop --json or write to a file")
	}

	if target == "-" {
		if _, err := client.DownloadFile(ctx, opts.board, id, stdout); err != nil {
			return failFile(opts.program, stderr, err, id)
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

	handle, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, downloadPerm)
	if err != nil {
		return fail(opts.program, stderr, fmt.Errorf("writing %s: %w", target, err))
	}

	written, err := client.DownloadFile(ctx, opts.board, id, handle)
	closeErr := handle.Close()
	if err != nil {
		if removeErr := os.Remove(target); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			writeMessage(stderr, "%s: removing the partial %s: %v\n", opts.program, target, removeErr)
		}
		return failFile(opts.program, stderr, err, id)
	}
	if closeErr != nil {
		return fail(opts.program, stderr, fmt.Errorf("closing %s: %w", target, closeErr))
	}

	if opts.json {
		if err := json.NewEncoder(stdout).Encode(struct {
			Board string `json:"board"`
			ID    string `json:"id"`
			Path  string `json:"path"`
			Bytes int64  `json:"bytes"`
		}{Board: opts.board, ID: id, Path: target, Bytes: written}); err != nil {
			return fail(opts.program, stderr, fmt.Errorf("writing the download result: %w", err))
		}
		return exitOK
	}

	notify(opts, stderr, "board %q: file %s saved to %s (%s)\n", opts.board, id, target, byteSize(written))
	return exitOK
}

// execFileDelete removes one attachment from the board. The id is the only
// argument: a delete of everything is the Clear action of the page, not
// something a mistyped command should be able to do.
func execFileDelete(ctx context.Context, client *boardclient.Client, opts options, positionals []string, stdout, stderr io.Writer) int {
	if len(positionals) != 1 {
		return usageError(opts.program, stderr, "file delete", "needs exactly one file id")
	}
	id := strings.TrimSpace(positionals[0])

	if err := client.DeleteFile(ctx, opts.board, id); err != nil {
		return failFile(opts.program, stderr, err, id)
	}

	if opts.json {
		if err := json.NewEncoder(stdout).Encode(struct {
			Board string `json:"board"`
			ID    string `json:"id"`
		}{Board: opts.board, ID: id}); err != nil {
			return fail(opts.program, stderr, fmt.Errorf("writing the delete result: %w", err))
		}
		return exitOK
	}

	notify(opts, stderr, "board %q: file %s deleted\n", opts.board, id)
	return exitOK
}

// failFile reports a failure of a single-file request, naming the id when the
// server does not know it, which is the common mistake of a typo or of an
// expired file.
func failFile(program string, stderr io.Writer, err error, id string) int {
	if errors.Is(err, boardclient.ErrFileNotFound) {
		return fail(program, stderr, fmt.Errorf("file %s not found on this board: it may have expired or been deleted", id))
	}
	return fail(program, stderr, err)
}

// byteSize renders a number of bytes for a human, with the units the rest of
// the CLI uses.
func byteSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}

	units := []string{"KiB", "MiB", "GiB", "TiB"}
	size := float64(bytes) / 1024
	unit := 0
	for size >= 1024 && unit < len(units)-1 {
		size /= 1024
		unit++
	}
	if size >= 10 {
		return fmt.Sprintf("%.0f %s", size, units[unit])
	}
	return fmt.Sprintf("%.1f %s", size, units[unit])
}
