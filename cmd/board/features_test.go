package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bassista/go_spin/internal/version"
)

func TestRunVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}, {"-V"}} {
		code, stdout, stderr := runCLI(t, args, "")
		if code != exitOK {
			t.Fatalf("%v: exit code %d, want %d (stderr %q)", args, code, exitOK, stderr)
		}
		if !strings.Contains(stdout, "gb-cli ") || !strings.Contains(stdout, version.Version) {
			t.Fatalf("%v: stdout %q does not carry the build metadata", args, stdout)
		}
	}

	code, _, stderr := runCLI(t, []string{"version", "extra"}, "")
	if code != exitUsage {
		t.Fatalf("exit code %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "unexpected argument") {
		t.Fatalf("stderr %q does not explain the misuse", stderr)
	}
}

func TestRunVersionJSON(t *testing.T) {
	code, stdout, stderr := runCLI(t, []string{"version", "--json"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}

	var report versionReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout %q is not a JSON object: %v", stdout, err)
	}
	if report.Program != "gb-cli" {
		t.Fatalf("program %q, want gb-cli", report.Program)
	}
	if report.Version != version.Version {
		t.Fatalf("version %q, want %q", report.Version, version.Version)
	}
	if report.GoVersion != version.GoVersion || report.GitCommit != version.GitCommit {
		t.Fatalf("build metadata %+v does not mirror the compiled values", report)
	}
	if stderr != "" {
		t.Fatalf("stderr %q, want nothing", stderr)
	}
}

func TestRunQuietSuppressesConfirmations(t *testing.T) {
	fake, server := newFakeBoards(t)

	code, stdout, stderr := runCLI(t, []string{"add", "-q", "-u", server.URL, "ciao"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr %q, want it empty with --quiet", stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout %q, want it empty", stdout)
	}
	if got := fake.text("default"); got != "ciao" {
		t.Fatalf("board text %q, want %q", got, "ciao")
	}
}

func TestRunAddJSON(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("default", "prima")

	code, stdout, stderr := runCLI(t, []string{"add", "--json", "-u", server.URL, "dopo"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr %q, want it empty with --json", stderr)
	}

	var result actionReport
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if result.Board != "default" || result.Action != "append" {
		t.Fatalf("report %+v", result)
	}
	if result.Bytes != 4 || result.TotalBytes != len("prima\ndopo") {
		t.Fatalf("report %+v, want 4 bytes added and %d in total", result, len("prima\ndopo"))
	}
}

func TestRunConfigJSON(t *testing.T) {
	code, stdout, stderr := runCLI(t, []string{"config", "--json", "-u", "http://board.example.com"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}

	var result configReport
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if result.ServerURL != "http://board.example.com" || result.Board != "default" {
		t.Fatalf("report %+v", result)
	}
	if result.TimeoutSeconds != 30 {
		t.Fatalf("timeout %d, want 30", result.TimeoutSeconds)
	}
}

func TestRunReadStat(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("lavoro", "contenuto")

	code, stdout, stderr := runCLI(t, []string{"read", "--stat", "-u", server.URL, "-b", "lavoro"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if stdout != "contenuto" {
		t.Fatalf("stdout %q, want the board text", stdout)
	}
	if !strings.Contains(stderr, `board "lavoro": revision 1, updated 2026-10-25T10:00:00Z`) {
		t.Fatalf("stderr %q does not carry the revision summary", stderr)
	}
}

func TestRunReadJSON(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("lavoro", "contenuto")

	code, stdout, stderr := runCLI(t, []string{"read", "--json", "-u", server.URL, "-b", "lavoro"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}

	var board struct {
		Name     string `json:"name"`
		Text     string `json:"text"`
		Revision uint64 `json:"revision"`
	}
	if err := json.Unmarshal([]byte(stdout), &board); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if board.Name != "lavoro" || board.Text != "contenuto" || board.Revision != 1 {
		t.Fatalf("board %+v", board)
	}
}

func TestRunUsesXDGConfigHome(t *testing.T) {
	fake, server := newFakeBoards(t)

	xdg := t.TempDir()
	configPath := filepath.Join(xdg, "ghostboard", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("cannot create the config directory: %v", err)
	}
	content := "url: " + server.URL + "\nboard: lavoro\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("cannot write the config file: %v", err)
	}

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("GO_SPIN_BOARD_URL", "")
	t.Setenv("GO_SPIN_BOARD_TIMEOUT_SECS", "")

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"add", "da xdg"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}
	if got := fake.text("lavoro"); got != "da xdg" {
		t.Fatalf("board text %q, want the text written through the XDG configuration", got)
	}
}

func TestRunNoConfigSkipsABrokenFile(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("default", "contenuto")

	home := t.TempDir()
	configPath := filepath.Join(home, ".config", "ghostboard", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("cannot create the config directory: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("url: [unclosed\n"), 0o600); err != nil {
		t.Fatalf("cannot write the config file: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("GO_SPIN_BOARD_URL", "")
	t.Setenv("GO_SPIN_BOARD_TIMEOUT_SECS", "")

	var stdout, stderr bytes.Buffer
	if code := run("gb-cli", []string{"read", "-u", server.URL}, strings.NewReader(""), &stdout, &stderr); code != exitError {
		t.Fatalf("exit code %d without --no-config, want %d", code, exitError)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run("gb-cli", []string{"read", "--no-config", "-u", server.URL}, strings.NewReader(""), &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code %d with --no-config, want %d (stderr %q)", code, exitOK, stderr.String())
	}
	if stdout.String() != "contenuto" {
		t.Fatalf("stdout %q, want the board text", stdout.String())
	}
}

func TestRunConfigPaths(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, ".config", "ghostboard", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("cannot create the config directory: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("url: http://board.example.com\n"), 0o600); err != nil {
		t.Fatalf("cannot write the config file: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GO_SPIN_BOARD_URL", "")
	t.Setenv("GO_SPIN_BOARD_TIMEOUT_SECS", "")

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"config", "--paths"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "looked up:") {
		t.Fatalf("stdout %q does not list the paths", out)
	}
	if !strings.Contains(out, configPath) || !strings.Contains(out, "found") {
		t.Fatalf("stdout %q does not mark the existing file", out)
	}
	if !strings.Contains(out, filepath.Join(filepath.Dir(executablePath()), "config.yaml")) {
		t.Fatalf("stdout %q does not list the executable directory", out)
	}

	stdout.Reset()
	stderr.Reset()
	code = run("gb-cli", []string{"config", "--paths", "--json"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}

	var result configReport
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout.String(), err)
	}
	if len(result.Paths) != 2 {
		t.Fatalf("paths %+v, want two candidates", result.Paths)
	}
}

func TestRunDownloadRefusesToOverwrite(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("lavoro", "contenuto")

	target := filepath.Join(t.TempDir(), "copia.txt")
	if err := os.WriteFile(target, []byte("precedente"), 0o600); err != nil {
		t.Fatalf("cannot write the target file: %v", err)
	}

	code, _, stderr := runCLI(t, []string{"download", "-u", server.URL, "-b", "lavoro", "-o", target}, "")
	if code != exitError {
		t.Fatalf("exit code %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "already exists") {
		t.Fatalf("stderr %q does not explain the refusal", stderr)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("cannot read the target file: %v", err)
	}
	if string(data) != "precedente" {
		t.Fatalf("file content %q, want it untouched", data)
	}

	code, _, stderr = runCLI(t, []string{"download", "--force", "-u", server.URL, "-b", "lavoro", "-o", target}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}

	data, err = os.ReadFile(target)
	if err != nil {
		t.Fatalf("cannot read the target file: %v", err)
	}
	if string(data) != "contenuto" {
		t.Fatalf("file content %q, want the board text", data)
	}
}

func TestRunUploadRejectsInvalidUTF8(t *testing.T) {
	_, server := newFakeBoards(t)

	path := filepath.Join(t.TempDir(), "binario.bin")
	if err := os.WriteFile(path, []byte{0xff, 0xfe, 0x00}, 0o600); err != nil {
		t.Fatalf("cannot write the file: %v", err)
	}

	code, _, stderr := runCLI(t, []string{"upload", "-u", server.URL, path}, "")
	if code != exitError {
		t.Fatalf("exit code %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "not valid UTF-8") {
		t.Fatalf("stderr %q does not explain the encoding", stderr)
	}
}

func TestRunDryRunWritesNothing(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("lavoro", "prima")
	fake.set("note", "testo")

	code, _, stderr := runCLI(t, []string{"add", "-u", server.URL, "-b", "lavoro", "--dry-run", "dopo"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if !strings.Contains(stderr, "dry run") {
		t.Fatalf("stderr %q does not mention the dry run", stderr)
	}
	if got := fake.text("lavoro"); got != "prima" {
		t.Fatalf("board text %q, want it untouched", got)
	}

	code, _, stderr = runCLI(t, []string{"set", "-u", server.URL, "-b", "note", "--dry-run", "altro"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if got := fake.text("note"); got != "testo" {
		t.Fatalf("board text %q, want it untouched", got)
	}
}

func TestRunEditWritesBack(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("lavoro", "base\n")

	script := filepath.Join(t.TempDir(), "editor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'aggiunto\\n' >> \"$1\"\n"), 0o700); err != nil {
		t.Fatalf("cannot write the editor script: %v", err)
	}

	t.Setenv("VISUAL", script)
	t.Setenv("EDITOR", "")

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"edit", "-u", server.URL, "-b", "lavoro"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}
	if got := fake.text("lavoro"); got != "base\naggiunto\n" {
		t.Fatalf("board text %q, want the edited text", got)
	}
	if !strings.Contains(stderr.String(), "replaced") {
		t.Fatalf("stderr %q does not confirm the write", stderr.String())
	}
}

func TestRunEditWithoutChanges(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("lavoro", "base\n")

	script := filepath.Join(t.TempDir(), "editor.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("cannot write the editor script: %v", err)
	}

	t.Setenv("VISUAL", script)
	t.Setenv("EDITOR", "")

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"edit", "-u", server.URL, "-b", "lavoro"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}
	if got := fake.text("lavoro"); got != "base\n" {
		t.Fatalf("board text %q, want it untouched", got)
	}
	if !strings.Contains(stderr.String(), "unchanged") {
		t.Fatalf("stderr %q does not report that nothing changed", stderr.String())
	}
}

func TestRunEditWithoutEditor(t *testing.T) {
	_, server := newFakeBoards(t)

	code, _, stderr := runCLI(t, []string{"edit", "-u", server.URL}, "")
	if code != exitError {
		t.Fatalf("exit code %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "no editor found") {
		t.Fatalf("stderr %q does not explain the missing editor", stderr)
	}
}

func TestRunCompletion(t *testing.T) {
	tests := []struct {
		shell string
		want  string
	}{
		{shell: "bash", want: "complete -F complete_gb_cli gb-cli"},
		{shell: "zsh", want: "compdef _gb_cli gb-cli"},
		{shell: "fish", want: "complete -c gb-cli"},
	}

	for _, test := range tests {
		t.Run(test.shell, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, []string{"completion", test.shell}, "")
			if code != exitOK {
				t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
			}
			if !strings.Contains(stdout, test.want) {
				t.Fatalf("stdout %q does not contain %q", stdout, test.want)
			}
		})
	}

	code, _, stderr := runCLI(t, []string{"completion"}, "")
	if code != exitUsage || !strings.Contains(stderr, "needs a shell") {
		t.Fatalf("exit code %d, stderr %q", code, stderr)
	}

	code, _, stderr = runCLI(t, []string{"completion", "tcsh"}, "")
	if code != exitUsage || !strings.Contains(stderr, "unsupported shell") {
		t.Fatalf("exit code %d, stderr %q", code, stderr)
	}
}

func TestRunFollowDiff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ghostboard/lavoro/events" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		frames := []string{
			"event: board\ndata: {\"name\":\"lavoro\",\"text\":\"uno\",\"revision\":1}\n\n",
			"event: board\ndata: {\"name\":\"lavoro\",\"text\":\"uno\\ndue\",\"revision\":2}\n\n",
			"event: board\ndata: {\"name\":\"lavoro\",\"text\":\"diverso\",\"revision\":3}\n\n",
		}
		for _, frame := range frames {
			if _, err := io.WriteString(w, frame); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	code, stdout, stderr := runCLI(t, []string{"follow", "--diff", "-u", server.URL, "-b", "lavoro"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}

	want := "uno\ndue\ndiverso\n"
	if stdout != want {
		t.Fatalf("stdout %q, want %q", stdout, want)
	}
}

func TestRunRetriesTransientFailure(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("default", "contenuto")
	fake.failNext(1, http.StatusServiceUnavailable)

	code, stdout, stderr := runCLI(t, []string{"read", "-u", server.URL, "--retry", "1"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if stdout != "contenuto" {
		t.Fatalf("stdout %q, want the board text", stdout)
	}
	if calls := fake.callCount(); calls < 2 {
		t.Fatalf("the server was called %d time(s), want at least 2", calls)
	}
}

func TestRunSendsConfiguredHeaders(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("default", "contenuto")

	code, _, stderr := runCLI(t, []string{"read", "-u", server.URL, "--header", "X-Debug: uno"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if got := fake.header("X-Debug"); got != "uno" {
		t.Fatalf("header X-Debug %q, want %q", got, "uno")
	}
}

func TestRunSendsTokenFromEnvironment(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("default", "contenuto")

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GO_SPIN_BOARD_URL", "")
	t.Setenv("GO_SPIN_BOARD_TIMEOUT_SECS", "")
	t.Setenv("GO_SPIN_BOARD_TOKEN", "segreto")

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"read", "-u", server.URL}, strings.NewReader(""), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}
	if got := fake.header("Authorization"); got != "Bearer segreto" {
		t.Fatalf("Authorization %q, want the bearer token", got)
	}
}

func TestRunAddMultipleFiles(t *testing.T) {
	fake, server := newFakeBoards(t)

	dir := t.TempDir()
	first := filepath.Join(dir, "primo.txt")
	second := filepath.Join(dir, "secondo.txt")
	if err := os.WriteFile(first, []byte("prima"), 0o600); err != nil {
		t.Fatalf("cannot write the first file: %v", err)
	}
	if err := os.WriteFile(second, []byte("seconda"), 0o600); err != nil {
		t.Fatalf("cannot write the second file: %v", err)
	}

	code, _, stderr := runCLI(t, []string{"add", "-u", server.URL, "-f", first, "-f", second}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if got := fake.text("default"); got != "prima\nseconda" {
		t.Fatalf("board text %q, want %q", got, "prima\nseconda")
	}
}

func TestRunExitCodes(t *testing.T) {
	fake, server := newFakeBoards(t)

	notFound := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(notFound.Close)

	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: "ok", args: []string{"read", "-u", server.URL}, want: exitOK},
		{name: "missing board api", args: []string{"read", "-u", notFound.URL}, want: exitEndpoint},
		{name: "invalid board", args: []string{"read", "-u", server.URL, "-b", "bad/name"}, want: exitBoardName},
		{name: "unknown command", args: []string{"frobnicate"}, want: exitUsage},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, _, stderr := runCLI(t, test.args, "")
			if code != test.want {
				t.Fatalf("exit code %d, want %d (stderr %q)", code, test.want, stderr)
			}
		})
	}

	if got := fake.text("default"); got != "" {
		t.Fatalf("board text %q, want it untouched", got)
	}
}
func TestRunDownloadStdoutRejectsJSON(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("lavoro", "contenuto")

	code, stdout, stderr := runCLI(t, []string{"download", "-u", server.URL, "-b", "lavoro", "-", "--json"}, "")
	if code != exitUsage {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitUsage, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout %q, want nothing: the text and the JSON would mix", stdout)
	}
	if !strings.Contains(stderr, "would share stdout") {
		t.Fatalf("stderr %q does not explain the conflict", stderr)
	}
}

func TestRunDownloadRejectsOutputFlagAndArgument(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("lavoro", "contenuto")

	dir := t.TempDir()
	target := filepath.Join(dir, "copia.txt")
	extra := filepath.Join(dir, "ignorato.txt")

	code, _, stderr := runCLI(t, []string{"download", "-u", server.URL, "-b", "lavoro", "-o", target, extra}, "")
	if code != exitUsage {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitUsage, stderr)
	}
	if !strings.Contains(stderr, "not both") {
		t.Fatalf("stderr %q does not explain the conflict", stderr)
	}
	for _, path := range []string{target, extra} {
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("%s was written despite the misuse", path)
		}
	}
}

func TestRunReadRejectsStatWithJSON(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("lavoro", "contenuto")

	code, stdout, stderr := runCLI(t, []string{"read", "-u", server.URL, "-b", "lavoro", "--stat", "--json"}, "")
	if code != exitUsage {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitUsage, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout %q, want nothing", stdout)
	}
	if !strings.Contains(stderr, "drop --stat") {
		t.Fatalf("stderr %q does not explain the conflict", stderr)
	}
}

func TestRunFollowRejectsJSON(t *testing.T) {
	fake, server := newFakeBoards(t)
	fake.set("lavoro", "contenuto")

	code, stdout, stderr := runCLI(t, []string{"follow", "-u", server.URL, "-b", "lavoro", "--json"}, "")
	if code != exitUsage {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitUsage, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout %q, want nothing", stdout)
	}
	if !strings.Contains(stderr, "--json is not supported") {
		t.Fatalf("stderr %q does not explain the refusal", stderr)
	}
}

func TestRunTerminatorProtectsATextLookingLikeAFlag(t *testing.T) {
	fake, server := newFakeBoards(t)

	for _, text := range []string{"--config", "--no-config", "-b"} {
		code, _, stderr := runCLI(t, []string{"set", "-u", server.URL, "-b", "lavoro", ""}, "")
		if code != exitOK {
			t.Fatalf("resetting the board: exit code %d (stderr %q)", code, stderr)
		}

		code, _, stderr = runCLI(t, []string{"add", "-u", server.URL, "-b", "lavoro", "--", text}, "")
		if code != exitOK {
			t.Fatalf("adding %q: exit code %d, want %d (stderr %q)", text, code, exitOK, stderr)
		}
		if got := fake.text("lavoro"); got != text {
			t.Fatalf("board text %q, want the literal %q", got, text)
		}
	}
}

func TestRunNoConfigAfterTheTerminatorIsStillRead(t *testing.T) {
	fake, server := newFakeBoards(t)

	home := t.TempDir()
	configPath := filepath.Join(home, ".config", "ghostboard", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("cannot create the config directory: %v", err)
	}
	content := "url: " + server.URL + "\nboard: lavoro\n"
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("cannot write the config file: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GO_SPIN_BOARD_URL", "")
	t.Setenv("GO_SPIN_BOARD_TIMEOUT_SECS", "")

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"add", "--", "--no-config"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}
	if got := fake.text("lavoro"); got != "--no-config" {
		t.Fatalf("board text %q: the literal text was read as a flag and the config was skipped", got)
	}
}

func TestRunReadJSONOmitsTheUpdateTimeOfAnUnwrittenBoard(t *testing.T) {
	_, server := newFakeBoards(t)

	code, stdout, stderr := runCLI(t, []string{"read", "-u", server.URL, "-b", "nuova", "--json"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if strings.Contains(stdout, "updated_at") {
		t.Fatalf("stdout %q carries an update time for a board that was never written", stdout)
	}

	var board struct {
		Revision uint64 `json:"revision"`
	}
	if err := json.Unmarshal([]byte(stdout), &board); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if board.Revision != 0 {
		t.Fatalf("revision %d, want 0", board.Revision)
	}
}

func TestRunConfigReportsUnknownKeys(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, ".config", "ghostboard", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("cannot create the config directory: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("url: http://board.example.com\ntimeout_sec: 5\n"), 0o600); err != nil {
		t.Fatalf("cannot write the config file: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GO_SPIN_BOARD_URL", "")
	t.Setenv("GO_SPIN_BOARD_TIMEOUT_SECS", "")

	var stdout, stderr bytes.Buffer
	if code := run("gb-cli", []string{"config"}, strings.NewReader(""), &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), `unknown key "timeout_sec"`) {
		t.Fatalf("stdout %q does not warn about the unknown key", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run("gb-cli", []string{"config", "--json"}, strings.NewReader(""), &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}

	var report configReport
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout.String(), err)
	}
	if len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0], "timeout_sec") {
		t.Fatalf("warnings %v, want one entry about the unknown key", report.Warnings)
	}
}

func TestRunConfigShowsTheEnvironmentOverrides(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GO_SPIN_BOARD_URL", "http://from-env.example.com")
	t.Setenv("GO_SPIN_BOARD_TIMEOUT_SECS", "")

	var stdout, stderr bytes.Buffer
	if code := run("gb-cli", []string{"config"}, strings.NewReader(""), &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "config file:  none found") {
		t.Fatalf("stdout %q does not start with the source", stdout.String())
	}
	if !strings.Contains(stdout.String(), "environment:  GO_SPIN_BOARD_URL") {
		t.Fatalf("stdout %q does not name the environment override", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run("gb-cli", []string{"config", "--json"}, strings.NewReader(""), &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}

	var report configReport
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout.String(), err)
	}
	if len(report.Environment) != 1 || report.Environment[0] != "GO_SPIN_BOARD_URL" {
		t.Fatalf("environment %v, want the overriding variable", report.Environment)
	}
}

func TestRunLastConfigFlagWins(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.yaml")
	second := filepath.Join(dir, "second.yaml")
	if err := os.WriteFile(first, []byte("url: http://first.example.com\nboard: primo\n"), 0o600); err != nil {
		t.Fatalf("cannot write the first config file: %v", err)
	}
	if err := os.WriteFile(second, []byte("url: http://second.example.com\nboard: secondo\n"), 0o600); err != nil {
		t.Fatalf("cannot write the second config file: %v", err)
	}

	code, stdout, stderr := runCLI(t, []string{"config", "--config", first, "--config", second}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitOK, stderr)
	}
	if !strings.Contains(stdout, "config file:  "+second) || !strings.Contains(stdout, "board:        secondo") {
		t.Fatalf("stdout %q does not reflect the last --config", stdout)
	}
}

func TestRunFlagMisuseExitsWithTheUsageCode(t *testing.T) {
	_, server := newFakeBoards(t)

	tests := []struct {
		name string
		args []string
	}{
		{name: "negative retry", args: []string{"read", "-u", server.URL, "--retry", "-1"}},
		{name: "zero timeout", args: []string{"read", "-u", server.URL, "--timeout", "0s"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, _, stderr := runCLI(t, test.args, "")
			if code != exitUsage {
				t.Fatalf("exit code %d, want %d (stderr %q)", code, exitUsage, stderr)
			}
		})
	}
}
