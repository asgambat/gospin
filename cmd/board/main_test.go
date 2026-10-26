package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeBoards mimics the board API of the server: the plain-text surface, the
// JSON state and a one-event event stream.
type fakeBoards struct {
	boards     map[string]string
	revisions  map[string]uint64
	headers    http.Header
	mu         sync.Mutex
	failStatus int
	failures   int
	calls      int
}

func newFakeBoards(t *testing.T) (*fakeBoards, *httptest.Server) {
	t.Helper()

	fake := &fakeBoards{boards: make(map[string]string), revisions: make(map[string]uint64)}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	return fake, server
}

func (f *fakeBoards) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls++
	f.headers = r.Header.Clone()
	if f.failStatus != 0 && f.failures > 0 {
		f.failures--
		status := f.failStatus
		f.mu.Unlock()
		w.WriteHeader(status)
		return
	}
	f.mu.Unlock()

	switch {
	case strings.HasPrefix(r.URL.Path, "/api/boards/"):
		f.serveText(w, r, strings.TrimPrefix(r.URL.Path, "/api/boards/"))
	case strings.HasSuffix(r.URL.Path, "/events"):
		f.serveEvents(w, strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/ghostboard/"), "/events"))
	case strings.HasPrefix(r.URL.Path, "/ghostboard/"):
		f.serveState(w, strings.TrimPrefix(r.URL.Path, "/ghostboard/"))
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeBoards) serveText(w http.ResponseWriter, r *http.Request, name string) {
	if name == "" {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		f.mu.Lock()
		text := f.boards[name]
		f.mu.Unlock()

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, text)
	case http.MethodPost:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "cannot read body", http.StatusBadRequest)
			return
		}

		f.mu.Lock()
		if f.boards[name] != string(body) {
			f.revisions[name]++
		}
		f.boards[name] = string(body)
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (f *fakeBoards) serveState(w http.ResponseWriter, name string) {
	f.mu.Lock()
	text := f.boards[name]
	revision := f.revisions[name]
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	encoded, err := json.Marshal(map[string]any{
		"name":       name,
		"text":       text,
		"revision":   revision,
		"updated_at": f.updatedAt(revision),
	})
	if err != nil {
		return
	}
	_, _ = w.Write(encoded)
}

func (f *fakeBoards) serveEvents(w http.ResponseWriter, name string) {
	f.mu.Lock()
	text := f.boards[name]
	revision := f.revisions[name]
	f.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	encoded, err := json.Marshal(map[string]any{
		"name":       name,
		"text":       text,
		"revision":   revision,
		"updated_at": f.updatedAt(revision),
	})
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: board\ndata: %s\n\n", encoded)
}

// updatedAt mirrors the server: a board that was never written carries the zero
// time, so the CLI has to drop it instead of printing a date in year 1.
func (f *fakeBoards) updatedAt(revision uint64) string {
	if revision == 0 {
		return "0001-01-01T00:00:00Z"
	}
	return "2026-10-25T10:00:00Z"
}

func (f *fakeBoards) set(board, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.boards[board] = text
	f.revisions[board]++
}

func (f *fakeBoards) text(board string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.boards[board]
}

func (f *fakeBoards) header(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.headers.Get(name)
}

func (f *fakeBoards) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// failNext makes the next count requests fail with status.
func (f *fakeBoards) failNext(count, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures = count
	f.failStatus = status
}

// runCLI executes the CLI with an isolated home directory and environment.
func runCLI(t *testing.T, args []string, stdin string) (int, string, string) {
	t.Helper()

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GO_SPIN_BOARD_URL", "")
	t.Setenv("GO_SPIN_BOARD_TIMEOUT_SECS", "")
	t.Setenv("GO_SPIN_BOARD_TOKEN", "")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", args, strings.NewReader(stdin), &stdout, &stderr)

	return code, stdout.String(), stderr.String()
}

func TestRunWithoutCommand(t *testing.T) {
	code, stdout, stderr := runCLI(t, nil, "")
	if code != exitUsage {
		t.Fatalf("exit code %d, want %d", code, exitUsage)
	}
	if stdout != "" {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Fatalf("stderr %q does not carry the usage", stderr)
	}
}

func TestRunHelp(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--help"}, {"help"}} {
		code, stdout, stderr := runCLI(t, args, "")
		if code != exitOK {
			t.Fatalf("%v: exit code %d, want %d (stderr %q)", args, code, exitOK, stderr)
		}
		if !strings.Contains(stdout, "gb-cli <command>") {
			t.Fatalf("%v: stdout %q does not carry the usage", args, stdout)
		}
	}
}

func TestRunHelpNeverContactsTheServer(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	home := t.TempDir()
	configPath := filepath.Join(home, ".config", "ghostboard", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("cannot create the config directory: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("url: "+server.URL+"\nboard: lavoro\n"), 0o600); err != nil {
		t.Fatalf("cannot write the config file: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("GO_SPIN_BOARD_URL", "")
	t.Setenv("GO_SPIN_BOARD_TIMEOUT_SECS", "")

	tests := []struct {
		name         string
		args         []string
		wantCode     int
		wantUsageOut bool
	}{
		{name: "short flag", args: []string{"-h"}, wantCode: exitOK, wantUsageOut: true},
		{name: "long flag", args: []string{"--help"}, wantCode: exitOK, wantUsageOut: true},
		{name: "help command", args: []string{"help"}, wantCode: exitOK, wantUsageOut: true},
		{name: "no arguments", args: nil, wantCode: exitUsage, wantUsageOut: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run("gb-cli", test.args, strings.NewReader(""), &stdout, &stderr)
			if code != test.wantCode {
				t.Fatalf("exit code %d, want %d (stderr %q)", code, test.wantCode, stderr.String())
			}

			usage := stdout.String()
			if !test.wantUsageOut {
				usage = stderr.String()
			}
			if !strings.Contains(usage, "gb-cli <command>") {
				t.Fatalf("usage %q does not explain how to use the CLI", usage)
			}
			if strings.Contains(stdout.String(), "404") || strings.Contains(stderr.String(), "404") {
				t.Fatalf("the help reported a 404: stdout %q, stderr %q", stdout.String(), stderr.String())
			}
		})
	}

	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("the server was contacted %d time(s) while only the help was needed", got)
	}
}

func TestRunExplainsAMissingBoardAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		if _, err := io.WriteString(w, "<html><body>404 page not found</body></html>"); err != nil {
			return
		}
	}))
	t.Cleanup(server.Close)

	code, _, stderr := runCLI(t, []string{"read", "-u", server.URL}, "")
	if code != exitEndpoint {
		t.Fatalf("exit code %d, want %d", code, exitEndpoint)
	}
	if !strings.Contains(stderr, "does not expose /api/boards/:board") {
		t.Fatalf("stderr %q does not explain the 404", stderr)
	}
	if strings.Contains(stderr, "page not found") || strings.Contains(stderr, "<html>") {
		t.Fatalf("stderr %q quotes the web page", stderr)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	code, _, stderr := runCLI(t, []string{"frobnicate"}, "")
	if code != exitUsage {
		t.Fatalf("exit code %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, `gb-cli: unknown command "frobnicate"`) {
		t.Fatalf("stderr %q does not name the command", stderr)
	}
}

func TestRunUsesTheInvokedProgramName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run("mio-board", []string{"-h"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}
	help := stdout.String()
	if !strings.Contains(help, "mio-board <command>") {
		t.Fatalf("help %q does not use the invoked name", help)
	}
	if strings.Contains(help, "gb-cli") {
		t.Fatalf("help %q still mentions the default name", help)
	}
	if !strings.Contains(help, "mio-board add \"note from the shell\"") {
		t.Fatalf("help %q does not adapt the examples", help)
	}

	stdout.Reset()
	stderr.Reset()
	code = run("mio-board", []string{"frobnicate"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("exit code %d, want %d", code, exitUsage)
	}
	if !strings.HasPrefix(stderr.String(), "mio-board: unknown command") {
		t.Fatalf("stderr %q does not start with the invoked name", stderr.String())
	}
}

func TestProgramName(t *testing.T) {
	original := os.Args
	t.Cleanup(func() { os.Args = original })

	tests := []struct {
		name string
		want string
		args []string
	}{
		{name: "plain", args: []string{"gb-cli"}, want: "gb-cli"},
		{name: "full path", args: []string{"/usr/local/bin/gb-cli", "read"}, want: "gb-cli"},
		{name: "windows extension", args: []string{"gb-cli.exe"}, want: "gb-cli"},
		{name: "no args", args: nil, want: defaultProgramName},
		{name: "empty arg", args: []string{""}, want: defaultProgramName},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			os.Args = test.args
			if got := programName(); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestRunAddAppendsTheArgument(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("default", "prima\n")

	code, stdout, stderr := runCLI(t, []string{"add", "-u", server.URL, "dopo"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if got := fake.text("default"); got != "prima\ndopo" {
		t.Fatalf("board text %q, want %q", got, "prima\ndopo")
	}
	if stdout != "" {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "updated") {
		t.Fatalf("stderr %q does not confirm the write", stderr)
	}
}

func TestRunAddSeparatesFromTheExistingText(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("default", "prima riga senza a capo")

	code, _, stderr := runCLI(t, []string{"add", "-u", server.URL, "dopo"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}

	want := "prima riga senza a capo\ndopo"
	if got := fake.text("default"); got != want {
		t.Fatalf("board text %q, want %q", got, want)
	}
}

func TestRunAddJoinsUnquotedWords(t *testing.T) {
	fake, server := newFakeBoards(t)

	code, _, stderr := runCLI(t, []string{"add", "-u", server.URL, "-b", "lavoro", "ciao", "mondo"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if got := fake.text("lavoro"); got != "ciao mondo" {
		t.Fatalf("board text %q, want %q", got, "ciao mondo")
	}
}

func TestRunAddFlagsAfterPositional(t *testing.T) {
	fake, server := newFakeBoards(t)

	code, _, stderr := runCLI(t, []string{"add", "ciao", "-u", server.URL, "-b", "lavoro"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if got := fake.text("lavoro"); got != "ciao" {
		t.Fatalf("board text %q, want %q", got, "ciao")
	}
}

func TestRunAddTextStartingWithDash(t *testing.T) {
	fake, server := newFakeBoards(t)

	code, _, stderr := runCLI(t, []string{"add", "-u", server.URL, "--", "-2 righe"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if got := fake.text("default"); got != "-2 righe" {
		t.Fatalf("board text %q, want %q", got, "-2 righe")
	}
}

func TestRunAddReadsStdin(t *testing.T) {
	fake, server := newFakeBoards(t)

	code, _, stderr := runCLI(t, []string{"add", "-u", server.URL}, "from the pipe\n")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if got := fake.text("default"); got != "from the pipe\n" {
		t.Fatalf("board text %q, want the piped text", got)
	}
}

func TestRunAddReadsFile(t *testing.T) {
	fake, server := newFakeBoards(t)

	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("da file\n"), 0o600); err != nil {
		t.Fatalf("cannot write the note: %v", err)
	}

	code, _, stderr := runCLI(t, []string{"add", "-u", server.URL, "-f", path}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if got := fake.text("default"); got != "da file\n" {
		t.Fatalf("board text %q, want the file content", got)
	}
}

func TestRunAddRejectsArgumentAndFile(t *testing.T) {
	_, server := newFakeBoards(t)

	code, _, stderr := runCLI(t, []string{"add", "-u", server.URL, "-f", "note.txt", "text"}, "")
	if code != exitUsage {
		t.Fatalf("exit code %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "not both") {
		t.Fatalf("stderr %q does not explain the conflict", stderr)
	}
}

func TestRunSetReplacesAndClears(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("note", "vecchio testo")

	code, _, stderr := runCLI(t, []string{"set", "-u", server.URL, "-b", "note", "nuovo"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if got := fake.text("note"); got != "nuovo" {
		t.Fatalf("board text %q, want %q", got, "nuovo")
	}

	code, _, stderr = runCLI(t, []string{"set", "-u", server.URL, "-b", "note", ""}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if got := fake.text("note"); got != "" {
		t.Fatalf("board text %q, want the board cleared", got)
	}
}

func TestRunReadWritesTheTextVerbatim(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("lavoro", "prima riga\nseconda riga")

	code, stdout, stderr := runCLI(t, []string{"read", "-u", server.URL, "-b", "lavoro"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if stdout != "prima riga\nseconda riga" {
		t.Fatalf("stdout %q, want the board text byte for byte", stdout)
	}
}

func TestRunReadEmptyBoard(t *testing.T) {
	_, server := newFakeBoards(t)

	code, stdout, stderr := runCLI(t, []string{"read", "-u", server.URL}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout %q, want an empty output", stdout)
	}
}

func TestRunReadRejectsArguments(t *testing.T) {
	_, server := newFakeBoards(t)

	code, _, stderr := runCLI(t, []string{"read", "-u", server.URL, "extra"}, "")
	if code != exitUsage {
		t.Fatalf("exit code %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "unexpected argument") {
		t.Fatalf("stderr %q does not explain the misuse", stderr)
	}
}

func TestRunUploadLoadsBoardFromFile(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("lavoro", "vecchio")

	path := filepath.Join(t.TempDir(), "draft.txt")
	if err := os.WriteFile(path, []byte("contenuto del file\n"), 0o600); err != nil {
		t.Fatalf("cannot write the draft: %v", err)
	}

	code, _, stderr := runCLI(t, []string{"upload", "-u", server.URL, "-b", "lavoro", path}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if got := fake.text("lavoro"); got != "contenuto del file\n" {
		t.Fatalf("board text %q, want the file content", got)
	}
}

func TestRunUploadAppends(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("lavoro", "prima\n")

	code, _, stderr := runCLI(t, []string{"upload", "-u", server.URL, "-b", "lavoro", "--append", "-"}, "dopo\n")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if got := fake.text("lavoro"); got != "prima\ndopo\n" {
		t.Fatalf("board text %q, want the appended text", got)
	}
}

func TestRunUploadNeedsOneFile(t *testing.T) {
	_, server := newFakeBoards(t)

	code, _, stderr := runCLI(t, []string{"upload", "-u", server.URL}, "")
	if code != exitUsage {
		t.Fatalf("exit code %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "exactly one file") {
		t.Fatalf("stderr %q does not explain the misuse", stderr)
	}
}

func TestRunDownloadDefaultFileName(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("lavoro", "contenuto")

	t.Chdir(t.TempDir())

	code, _, stderr := runCLI(t, []string{"download", "-u", server.URL, "-b", "lavoro"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}

	data, err := os.ReadFile("lavoro.txt")
	if err != nil {
		t.Fatalf("cannot read the downloaded file: %v", err)
	}
	if string(data) != "contenuto" {
		t.Fatalf("file content %q, want the board text", data)
	}
}

func TestRunDownloadExplicitFileAndStdout(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("lavoro", "riga uno\nriga due")

	target := filepath.Join(t.TempDir(), "copia.txt")
	code, _, stderr := runCLI(t, []string{"download", "-u", server.URL, "-b", "lavoro", "-o", target}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("cannot read the downloaded file: %v", err)
	}
	if string(data) != "riga uno\nriga due" {
		t.Fatalf("file content %q, want the board text", data)
	}

	code, stdout, stderr := runCLI(t, []string{"download", "-u", server.URL, "-b", "lavoro", "-"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if stdout != "riga uno\nriga due" {
		t.Fatalf("stdout %q, want the board text", stdout)
	}
}

func TestRunConfigPrintsTheEffectiveConfiguration(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, ".config", "ghostboard", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("cannot create the config directory: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("url: http://board.example.com:9000\nboard: lavoro\ntimeout_secs: 7\n"), 0o600); err != nil {
		t.Fatalf("cannot write the config file: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("GO_SPIN_BOARD_URL", "")
	t.Setenv("GO_SPIN_BOARD_TIMEOUT_SECS", "")

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"config"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{configPath, "http://board.example.com:9000", "lavoro", "7s"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestRunConfigShowsAFlagOverrideAndAnInvalidURL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GO_SPIN_BOARD_URL", "")
	t.Setenv("GO_SPIN_BOARD_TIMEOUT_SECS", "")

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"config", "-u", "https://gospin.lan.asga.uk/ui/dashboard/"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitError {
		t.Fatalf("exit code %d, want %d", code, exitError)
	}

	out := stdout.String()
	if !strings.Contains(out, "none found, using the built-in defaults") {
		t.Fatalf("output %q does not name the configuration source", out)
	}
	if !strings.Contains(out, "https://gospin.lan.asga.uk/ui/dashboard/") {
		t.Fatalf("output %q does not show the URL in use", out)
	}
	if !strings.Contains(stderr.String(), "GoSpin page") {
		t.Fatalf("stderr %q does not explain the invalid URL", stderr.String())
	}
}

func TestRunConfigIsOffline(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"config", "-u", server.URL}, strings.NewReader(""), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("config contacted the server %d time(s)", got)
	}
}

func TestRunConfigRejectsArguments(t *testing.T) {
	code, _, stderr := runCLI(t, []string{"config", "extra"}, "")
	if code != exitUsage {
		t.Fatalf("exit code %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "unexpected argument") {
		t.Fatalf("stderr %q does not explain the misuse", stderr)
	}
}

func TestRunFollowPrintsEveryRevision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ghostboard/lavoro/events" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")

		frames := []string{
			"event: board\ndata: {\"name\":\"lavoro\",\"text\":\"prima\",\"revision\":1}\n\n",
			"event: board\ndata: {\"name\":\"lavoro\",\"text\":\"prima\\ndopo\\n\",\"revision\":2}\n\n",
		}
		for _, frame := range frames {
			if _, err := io.WriteString(w, frame); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	code, stdout, stderr := runCLI(t, []string{"follow", "-u", server.URL, "-b", "lavoro"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}

	want := "prima\nprima\ndopo\n"
	if stdout != want {
		t.Fatalf("stdout %q, want %q", stdout, want)
	}
	if !strings.Contains(stderr, `following board "lavoro"`) {
		t.Fatalf("stderr %q does not announce the stream", stderr)
	}
}

func TestRunFollowRejectsArguments(t *testing.T) {
	_, server := newFakeBoards(t)

	code, _, stderr := runCLI(t, []string{"follow", "-u", server.URL, "extra"}, "")
	if code != exitUsage {
		t.Fatalf("exit code %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "unexpected argument") {
		t.Fatalf("stderr %q does not explain the misuse", stderr)
	}
}

func TestRunReportsServerErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "text exceeds the maximum allowed size"})
	}))
	t.Cleanup(server.Close)

	code, _, stderr := runCLI(t, []string{"add", "-u", server.URL, "troppo"}, "")
	if code != exitError {
		t.Fatalf("exit code %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "text exceeds the maximum allowed size") {
		t.Fatalf("stderr %q does not carry the server message", stderr)
	}
}

func TestRunRejectsInvalidBoardName(t *testing.T) {
	_, server := newFakeBoards(t)

	code, _, stderr := runCLI(t, []string{"read", "-u", server.URL, "-b", "bad/name"}, "")
	if code != exitBoardName {
		t.Fatalf("exit code %d, want %d", code, exitBoardName)
	}
	if !strings.Contains(stderr, "invalid board name") {
		t.Fatalf("stderr %q does not explain the board name", stderr)
	}
}

func TestRunReportsUnreachableServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()

	code, _, stderr := runCLI(t, []string{"read", "-u", url}, "")
	if code != exitError {
		t.Fatalf("exit code %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "contacting the GoSpin server") {
		t.Fatalf("stderr %q does not explain the failure", stderr)
	}
}

func TestRunUsesTheUserConfigFile(t *testing.T) {
	fake, server := newFakeBoards(t)

	home := t.TempDir()
	configPath := filepath.Join(home, ".config", "ghostboard", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("cannot create the config directory: %v", err)
	}
	content := "url: " + server.URL + "\nboard: lavoro\ntimeout_secs: 5\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("cannot write the config file: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("GO_SPIN_BOARD_URL", "")
	t.Setenv("GO_SPIN_BOARD_TIMEOUT_SECS", "")

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"add", "dalla config"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}
	if got := fake.text("lavoro"); got != "dalla config" {
		t.Fatalf("board text %q, want the text written on the configured board", got)
	}
}

func TestRunEnvironmentOverridesTheConfigFile(t *testing.T) {
	fake, server := newFakeBoards(t)

	home := t.TempDir()
	configPath := filepath.Join(home, ".config", "ghostboard", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("cannot create the config directory: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("url: http://127.0.0.1:1\n"), 0o600); err != nil {
		t.Fatalf("cannot write the config file: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("GO_SPIN_BOARD_URL", server.URL)
	t.Setenv("GO_SPIN_BOARD_TIMEOUT_SECS", "")

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"add", "via env"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}
	if got := fake.text("default"); got != "via env" {
		t.Fatalf("board text %q, want the text written through the environment URL", got)
	}
}

func TestRunMissingExplicitConfigFile(t *testing.T) {
	code, _, stderr := runCLI(t, []string{"read", "--config", filepath.Join(t.TempDir(), "missing.yaml")}, "")
	if code != exitError {
		t.Fatalf("exit code %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "config file") {
		t.Fatalf("stderr %q does not name the config file", stderr)
	}
}

func TestRunFlagOverridesAConfigValue(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("default", "contenuto")

	home := t.TempDir()
	configPath := filepath.Join(home, ".config", "ghostboard", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("cannot create the config directory: %v", err)
	}
	content := "url: \"https://gospin.lan.asga.uk/ui/dashboard/\"\nboard: bad/name\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("cannot write the config file: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("GO_SPIN_BOARD_URL", "")
	t.Setenv("GO_SPIN_BOARD_TIMEOUT_SECS", "")

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"read"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitError {
		t.Fatalf("exit code %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr.String(), "GoSpin page") {
		t.Fatalf("stderr %q does not reject the page URL of the file", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run("gb-cli", []string{"read", "-u", server.URL, "-b", "default"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}
	if stdout.String() != "contenuto" {
		t.Fatalf("stdout %q, want the board text", stdout.String())
	}
}

func TestRunRejectsADashboardPageURL(t *testing.T) {
	code, _, stderr := runCLI(t, []string{"read", "-u", "https://gospin.lan.asga.uk/ui/dashboard/"}, "")
	if code != exitError {
		t.Fatalf("exit code %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "GoSpin page") {
		t.Fatalf("stderr %q does not explain that a page URL was given", stderr)
	}
	if !strings.Contains(stderr, "https://gospin.lan.asga.uk") {
		t.Fatalf("stderr %q does not suggest the server root", stderr)
	}
}

func TestRunInvalidURL(t *testing.T) {
	code, _, stderr := runCLI(t, []string{"read", "-u", "not-a-url"}, "")
	if code != exitError {
		t.Fatalf("exit code %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "invalid server URL") {
		t.Fatalf("stderr %q does not explain the URL", stderr)
	}
}
