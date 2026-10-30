package main

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeFiles mimics the plain attachment API of the server.
type fakeFiles struct {
	files    map[string]string
	names    map[string]string
	archive  string
	received string
	deleted  string
	mu       sync.Mutex
}

func newFakeFiles(t *testing.T) (*fakeFiles, *httptest.Server) {
	t.Helper()

	fake := &fakeFiles{files: make(map[string]string), names: make(map[string]string)}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, server
}

func (f *fakeFiles) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/boards/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || (parts[1] != "files" && parts[1] != "files.zip") {
		http.NotFound(w, r)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if len(parts) == 2 && parts[1] == "files.zip" && r.Method == http.MethodGet {
		if len(f.files) == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"board has no files"}`))
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write([]byte(f.archive))
		return
	}

	switch {
	case len(parts) == 2 && r.Method == http.MethodGet:
		var body strings.Builder
		for id, content := range f.files {
			body.WriteString(id + "\t" + f.names[id] + "\t" + itoa(len(content)) + "\t2026-10-30T00:00:00Z\n")
		}
		_, _ = w.Write([]byte(body.String()))
	case len(parts) == 2 && r.Method == http.MethodPost:
		reader, err := r.MultipartReader()
		if err != nil {
			http.Error(w, "not multipart", http.StatusBadRequest)
			return
		}
		part, err := reader.NextPart()
		if err != nil {
			http.Error(w, "no part", http.StatusBadRequest)
			return
		}
		content, err := io.ReadAll(part)
		if err != nil {
			http.Error(w, "cannot read", http.StatusBadRequest)
			return
		}
		id := "aaaaaaaaaaaaaaaaaaaaaaaaaa"
		f.files[id] = string(content)
		f.names[id] = part.FileName()
		f.received = string(content)

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(id + "\t" + part.FileName() + "\t" + itoa(len(content)) + "\t2026-10-30T00:00:00Z\n"))
	case len(parts) == 3 && r.Method == http.MethodGet:
		content, ok := f.files[parts[2]]
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"file not found"}`))
			return
		}
		_, _ = w.Write([]byte(content))
	case len(parts) == 3 && r.Method == http.MethodDelete:
		if _, ok := f.files[parts[2]]; !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"file not found"}`))
			return
		}
		delete(f.files, parts[2])
		delete(f.names, parts[2])
		f.deleted = parts[2]
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

func TestRunFileUpload(t *testing.T) {
	fake, server := newFakeFiles(t)
	path := writeTempFile(t, "note.txt", "hello file")

	code, stdout, stderr := runCLI(t, []string{"-u", server.URL, "file", "upload", "-b", "team", path}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want 0 (stderr %q)", code, stderr)
	}
	if !strings.Contains(stdout, "aaaaaaaaaaaaaaaaaaaaaaaaaa") || !strings.Contains(stdout, "note.txt") {
		t.Errorf("stdout %q does not name the stored file", stdout)
	}
	if fake.received != "hello file" {
		t.Errorf("server received %q, want the file content", fake.received)
	}
}

func TestRunFileUploadQuietPrintsOnlyTheID(t *testing.T) {
	_, server := newFakeFiles(t)
	path := writeTempFile(t, "note.txt", "x")

	code, stdout, stderr := runCLI(t, []string{"-u", server.URL, "file", "upload", "-q", "-b", "team", path}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want 0 (stderr %q)", code, stderr)
	}
	if stdout != "aaaaaaaaaaaaaaaaaaaaaaaaaa\n" {
		t.Errorf("stdout = %q, want only the id", stdout)
	}
}

func TestRunFileUploadJSON(t *testing.T) {
	_, server := newFakeFiles(t)
	path := writeTempFile(t, "note.txt", "abc")

	code, stdout, stderr := runCLI(t, []string{"-u", server.URL, "file", "upload", "--json", "-b", "team", path}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want 0 (stderr %q)", code, stderr)
	}

	var file struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal([]byte(stdout), &file); err != nil {
		t.Fatalf("decoding %q: %v", stdout, err)
	}
	if file.ID == "" || file.Name != "note.txt" || file.Size != 3 {
		t.Errorf("upload report = %+v, want the stored metadata", file)
	}
}

func TestRunFileList(t *testing.T) {
	fake, server := newFakeFiles(t)
	fake.files["aaaaaaaaaaaaaaaaaaaaaaaaaa"] = "hello"
	fake.names["aaaaaaaaaaaaaaaaaaaaaaaaaa"] = "hello.txt"

	code, stdout, stderr := runCLI(t, []string{"-u", server.URL, "file", "list", "-b", "team"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want 0 (stderr %q)", code, stderr)
	}
	for _, want := range []string{"ID", "hello.txt", "5 B"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout %q does not carry %q", stdout, want)
		}
	}
}

func TestRunFileListJSON(t *testing.T) {
	fake, server := newFakeFiles(t)
	fake.files["aaaaaaaaaaaaaaaaaaaaaaaaaa"] = "hello"
	fake.names["aaaaaaaaaaaaaaaaaaaaaaaaaa"] = "hello.txt"

	code, stdout, stderr := runCLI(t, []string{"-u", server.URL, "file", "list", "--json", "-b", "team"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want 0 (stderr %q)", code, stderr)
	}

	var files []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal([]byte(stdout), &files); err != nil {
		t.Fatalf("decoding %q: %v", stdout, err)
	}
	if len(files) != 1 || files[0].Name != "hello.txt" {
		t.Errorf("file list = %+v, want one entry", files)
	}
}

func TestRunFileGetToFile(t *testing.T) {
	fake, server := newFakeFiles(t)
	fake.files["aaaaaaaaaaaaaaaaaaaaaaaaaa"] = "downloaded bytes"
	fake.names["aaaaaaaaaaaaaaaaaaaaaaaaaa"] = "note.txt"

	target := filepath.Join(t.TempDir(), "out.txt")
	code, _, stderr := runCLI(t, []string{"-u", server.URL, "file", "get", "-b", "team", "-o", target, "aaaaaaaaaaaaaaaaaaaaaaaaaa"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want 0 (stderr %q)", code, stderr)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading the downloaded file: %v", err)
	}
	if string(data) != "downloaded bytes" {
		t.Errorf("downloaded %q, want the stored bytes", data)
	}
	if !strings.Contains(stderr, "saved to") {
		t.Errorf("stderr %q does not confirm the download", stderr)
	}

	// The file exists: without --force the command refuses to overwrite it.
	code, _, stderr = runCLI(t, []string{"-u", server.URL, "file", "get", "-b", "team", "-o", target, "aaaaaaaaaaaaaaaaaaaaaaaaaa"}, "")
	if code != exitError {
		t.Fatalf("exit code %d, want %d when the target exists (stderr %q)", code, exitError, stderr)
	}
	if !strings.Contains(stderr, "--force") {
		t.Errorf("stderr %q does not point at --force", stderr)
	}

	code, _, _ = runCLI(t, []string{"-u", server.URL, "file", "get", "-b", "team", "--force", "-o", target, "aaaaaaaaaaaaaaaaaaaaaaaaaa"}, "")
	if code != exitOK {
		t.Errorf("exit code %d with --force, want 0", code)
	}
}

func TestRunFileGetToStdout(t *testing.T) {
	fake, server := newFakeFiles(t)
	fake.files["aaaaaaaaaaaaaaaaaaaaaaaaaa"] = "to stdout"

	code, stdout, stderr := runCLI(t, []string{"-u", server.URL, "file", "get", "-b", "team", "aaaaaaaaaaaaaaaaaaaaaaaaaa"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want 0 (stderr %q)", code, stderr)
	}
	if stdout != "to stdout" {
		t.Errorf("stdout = %q, want the file bytes", stdout)
	}
}

func TestRunFileGetAllDefaultsToTheBoardName(t *testing.T) {
	fake, server := newFakeFiles(t)
	fake.files["aaaaaaaaaaaaaaaaaaaaaaaaaa"] = "hello"
	fake.archive = "PK archive bytes"

	t.Chdir(t.TempDir())
	code, stdout, stderr := runCLI(t, []string{"-u", server.URL, "file", "get", "--all", "-b", "team"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want 0 (stderr %q)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want the archive in the file", stdout)
	}

	data, err := os.ReadFile("team.zip")
	if err != nil {
		t.Fatalf("reading the archive: %v", err)
	}
	if string(data) != fake.archive {
		t.Errorf("archive = %q, want the server bytes", data)
	}
	if !strings.Contains(stderr, "files saved to team.zip") {
		t.Errorf("stderr %q does not confirm the download", stderr)
	}
}

func TestRunFileGetAllToFile(t *testing.T) {
	fake, server := newFakeFiles(t)
	fake.files["aaaaaaaaaaaaaaaaaaaaaaaaaa"] = "hello"
	fake.archive = "PK archive"

	target := filepath.Join(t.TempDir(), "board.zip")
	code, _, stderr := runCLI(t, []string{"-u", server.URL, "file", "get", "--all", "-b", "team", "-o", target}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want 0 (stderr %q)", code, stderr)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading the archive: %v", err)
	}
	if string(data) != fake.archive {
		t.Errorf("archive = %q, want the server bytes", data)
	}

	// An existing target is only replaced with --force.
	code, _, stderr = runCLI(t, []string{"-u", server.URL, "file", "get", "--all", "-b", "team", "-o", target}, "")
	if code != exitError {
		t.Fatalf("exit code %d, want %d when the target exists (stderr %q)", code, exitError, stderr)
	}
	if !strings.Contains(stderr, "--force") {
		t.Errorf("stderr %q does not point at --force", stderr)
	}

	code, _, stderr = runCLI(t, []string{"-u", server.URL, "file", "get", "--all", "-b", "team", "--force", "-o", target}, "")
	if code != exitOK {
		t.Errorf("exit code %d with --force, want 0 (stderr %q)", code, stderr)
	}
}

func TestRunFileGetAllToStdout(t *testing.T) {
	fake, server := newFakeFiles(t)
	fake.files["aaaaaaaaaaaaaaaaaaaaaaaaaa"] = "hello"
	fake.archive = "PK to stdout"

	code, stdout, stderr := runCLI(t, []string{"-u", server.URL, "file", "get", "--all", "-b", "team", "-o", "-"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want 0 (stderr %q)", code, stderr)
	}
	if stdout != fake.archive {
		t.Errorf("stdout = %q, want the archive bytes", stdout)
	}
}

func TestRunFileGetAllJSON(t *testing.T) {
	fake, server := newFakeFiles(t)
	fake.files["aaaaaaaaaaaaaaaaaaaaaaaaaa"] = "hello"
	fake.archive = "PK archive"

	t.Chdir(t.TempDir())
	code, stdout, stderr := runCLI(t, []string{"-u", server.URL, "file", "get", "--all", "--json", "-b", "team"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want 0 (stderr %q)", code, stderr)
	}

	var report struct {
		Board string `json:"board"`
		Path  string `json:"path"`
		Bytes int64  `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decoding %q: %v", stdout, err)
	}
	if report.Board != "team" || report.Path != "team.zip" || report.Bytes != int64(len(fake.archive)) {
		t.Errorf("archive report = %+v, want team.zip of %d bytes", report, len(fake.archive))
	}
}

func TestRunFileGetAllEmptyBoard(t *testing.T) {
	_, server := newFakeFiles(t)

	t.Chdir(t.TempDir())
	code, stdout, stderr := runCLI(t, []string{"-u", server.URL, "file", "get", "--all", "-b", "team"}, "")
	if code != exitError {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitError, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if !strings.Contains(stderr, "has no files") {
		t.Errorf("stderr %q does not say the board is empty", stderr)
	}
	if _, err := os.Stat("team.zip"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("an empty board left a file behind: %v", err)
	}
}

func TestRunFileGetAllUsage(t *testing.T) {
	tests := [][]string{
		{"file", "get", "--all", "aaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{"file", "get", "--all", "--json", "-o", "-"},
	}

	for _, args := range tests {
		code, _, stderr := runCLI(t, args, "")
		if code != exitUsage {
			t.Errorf("%v: exit code %d, want %d (stderr %q)", args, code, exitUsage, stderr)
		}
	}
}

func TestRunFileGetMissingFile(t *testing.T) {
	_, server := newFakeFiles(t)

	code, stdout, stderr := runCLI(t, []string{"-u", server.URL, "file", "get", "-b", "team", "aaaaaaaaaaaaaaaaaaaaaaaaaa"}, "")
	if code != exitError {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitError, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing on a failed download", stdout)
	}
	if !strings.Contains(stderr, "not found") {
		t.Errorf("stderr %q does not say the file is missing", stderr)
	}
}

func TestRunFileDelete(t *testing.T) {
	fake, server := newFakeFiles(t)
	fake.files["aaaaaaaaaaaaaaaaaaaaaaaaaa"] = "bye"
	fake.names["aaaaaaaaaaaaaaaaaaaaaaaaaa"] = "note.txt"

	code, stdout, stderr := runCLI(t, []string{"-u", server.URL, "file", "delete", "-b", "team", "aaaaaaaaaaaaaaaaaaaaaaaaaa"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want 0 (stderr %q)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing on a delete", stdout)
	}
	if fake.deleted != "aaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("server deleted %q, want the requested id", fake.deleted)
	}
	if !strings.Contains(stderr, "deleted") {
		t.Errorf("stderr %q does not confirm the delete", stderr)
	}
}

func TestRunFileDeleteQuiet(t *testing.T) {
	fake, server := newFakeFiles(t)
	fake.files["aaaaaaaaaaaaaaaaaaaaaaaaaa"] = "bye"

	code, stdout, stderr := runCLI(t, []string{"-u", server.URL, "file", "delete", "-q", "-b", "team", "aaaaaaaaaaaaaaaaaaaaaaaaaa"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want 0 (stderr %q)", code, stderr)
	}
	if stdout != "" || stderr != "" {
		t.Errorf("stdout %q stderr %q, want a silent delete with -q", stdout, stderr)
	}
}

func TestRunFileDeleteJSON(t *testing.T) {
	fake, server := newFakeFiles(t)
	fake.files["aaaaaaaaaaaaaaaaaaaaaaaaaa"] = "bye"

	code, stdout, stderr := runCLI(t, []string{"-u", server.URL, "file", "delete", "--json", "-b", "team", "aaaaaaaaaaaaaaaaaaaaaaaaaa"}, "")
	if code != exitOK {
		t.Fatalf("exit code %d, want 0 (stderr %q)", code, stderr)
	}

	var report struct {
		Board string `json:"board"`
		ID    string `json:"id"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decoding %q: %v", stdout, err)
	}
	if report.Board != "team" || report.ID != "aaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("delete report = %+v, want the deleted id", report)
	}
}

func TestRunFileDeleteMissingFile(t *testing.T) {
	_, server := newFakeFiles(t)

	code, stdout, stderr := runCLI(t, []string{"-u", server.URL, "file", "delete", "-b", "team", "aaaaaaaaaaaaaaaaaaaaaaaaaa"}, "")
	if code != exitError {
		t.Fatalf("exit code %d, want %d (stderr %q)", code, exitError, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing on a failed delete", stdout)
	}
	if !strings.Contains(stderr, "not found") {
		t.Errorf("stderr %q does not say the file is missing", stderr)
	}
}

func TestRunFileUsage(t *testing.T) {
	tests := [][]string{
		{"file"},
		{"file", "nope"},
		{"file", "upload"},
		{"file", "get"},
		{"file", "delete"},
	}

	for _, args := range tests {
		code, _, stderr := runCLI(t, args, "")
		if code != exitUsage {
			t.Errorf("%v: exit code %d, want %d (stderr %q)", args, code, exitUsage, stderr)
		}
	}
}
