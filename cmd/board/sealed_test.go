package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/asgambat/gospin/internal/api/controller"
	"github.com/asgambat/gospin/internal/ghostboard"
)

// newSealedCLIServer serves the real board routes over a real sealed store, so the
// CLI is exercised against the behaviour it meets in production rather than
// against a hand-written stub.
func newSealedCLIServer(t *testing.T) *httptest.Server {
	t.Helper()

	store, err := ghostboard.New(ghostboard.Config{
		ClearDelay:      time.Minute,
		SealedTombstone: time.Hour,
		MaxTextBytes:    65536,
		PersistInterval: time.Second,
		SealedEnabled:   true,
	})
	if err != nil {
		t.Fatalf("ghostboard.New() error = %v", err)
	}

	gc := controller.NewGhostboardController(store, nil, 65536, time.Second)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ghostboard/:board", gc.Get)
	r.POST("/ghostboard/:board", gc.Update)
	r.GET("/ghostboard/:board/events", gc.Events)
	r.GET("/api/boards/:board", gc.GetText)
	r.POST("/api/boards/:board", gc.SetText)
	r.POST("/ghostboard/:board/open", gc.Open)
	r.POST("/api/boards/:board/open", gc.OpenText)

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return server
}

// runCLIWithKey runs the CLI with GO_SPIN_BOARD_KEY set, the way a script would
// export the key once instead of repeating it on every command.
func runCLIWithKey(t *testing.T, args []string, key, stdin string) (int, string, string) {
	t.Helper()

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GO_SPIN_BOARD_URL", "")
	t.Setenv("GO_SPIN_BOARD_TIMEOUT_SECS", "")
	t.Setenv("GO_SPIN_BOARD_PIN", "")
	t.Setenv("GO_SPIN_BOARD_KEY", key)

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", args, strings.NewReader(stdin), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// sealedKeyFrom parses the key out of the link printed by sealed new.
func sealedKeyFrom(t *testing.T, stdout string) string {
	t.Helper()

	_, fragment, found := strings.Cut(strings.TrimSpace(stdout), "#k=")
	if !found || fragment == "" {
		t.Fatalf("sealed new printed no link with a key: %q", stdout)
	}
	return strings.TrimSpace(strings.Split(fragment, "\n")[0])
}

func TestSealedNewPrintsALinkAndItsKey(t *testing.T) {
	server := newSealedCLIServer(t)

	code, stdout, stderr := runCLIWithKey(t, []string{
		"sealed", "new", "-b", "nota", "-u", server.URL, "--burn=false", "la password del wifi",
	}, "", "")
	if code != exitOK {
		t.Fatalf("exit code %d, want 0: %s", code, stderr)
	}

	link := strings.TrimSpace(stdout)
	if !strings.HasPrefix(link, server.URL+"/ui/sealed/nota#k=") {
		t.Fatalf("the link is %q, want the sealed page of the board", link)
	}
	if !strings.Contains(stderr, "key:") {
		t.Fatalf("the key was not printed on stderr: %s", stderr)
	}
	if !strings.Contains(stderr, "cannot recover it") {
		t.Fatalf("the warning about the key is missing: %s", stderr)
	}

	// The same key reads the board back, and the server only ever held the
	// envelope.
	key := sealedKeyFrom(t, stdout)
	code, stdout, stderr = runCLIWithKey(t, []string{
		"read", "-b", "nota", "-u", server.URL, "--key", key,
	}, "", "")
	if code != exitOK {
		t.Fatalf("read exit code %d, want 0: %s", code, stderr)
	}
	if stdout != "la password del wifi" {
		t.Fatalf("read printed %q, want the sealed text", stdout)
	}
}

func TestSealedNewJSONReport(t *testing.T) {
	server := newSealedCLIServer(t)

	code, stdout, stderr := runCLIWithKey(t, []string{
		"sealed", "new", "-b", "nota", "-u", server.URL, "--json", "segreto",
	}, "", "")
	if code != exitOK {
		t.Fatalf("exit code %d, want 0: %s", code, stderr)
	}

	var report struct {
		Board string `json:"board"`
		Mode  string `json:"mode"`
		Link  string `json:"link"`
		Key   string `json:"key"`
		Burn  bool   `json:"burn"`
		Bytes int    `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decoding the report: %v", err)
	}

	if report.Board != "nota" || report.Mode != "sealed" || !report.Burn || report.Bytes != len("segreto") {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.Key == "" || !strings.HasSuffix(report.Link, "#k="+report.Key) {
		t.Fatalf("the report does not carry the key and the link: %+v", report)
	}
}

func TestSealedRoundTripWithKeySources(t *testing.T) {
	server := newSealedCLIServer(t)

	code, stdout, stderr := runCLIWithKey(t, []string{
		"sealed", "new", "-b", "nota", "-u", server.URL, "--burn=false", "--json", "prima riga",
	}, "", "")
	if code != exitOK {
		t.Fatalf("sealed new exit code %d: %s", code, stderr)
	}

	var report struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decoding the report: %v", err)
	}

	keyPath := filepath.Join(t.TempDir(), "nota.key")
	if err := os.WriteFile(keyPath, []byte(report.Key+"\n"), 0o600); err != nil {
		t.Fatalf("writing the key file: %v", err)
	}

	t.Run("key from the environment", func(t *testing.T) {
		code, stdout, stderr := runCLIWithKey(t, []string{
			"read", "-b", "nota", "-u", server.URL,
		}, report.Key, "")
		if code != exitOK {
			t.Fatalf("exit code %d: %s", code, stderr)
		}
		if stdout != "prima riga" {
			t.Fatalf("read printed %q", stdout)
		}
	})

	t.Run("key from a file", func(t *testing.T) {
		code, stdout, stderr := runCLIWithKey(t, []string{
			"read", "-b", "nota", "-u", server.URL, "--key-file", keyPath,
		}, "", "")
		if code != exitOK {
			t.Fatalf("exit code %d: %s", code, stderr)
		}
		if stdout != "prima riga" {
			t.Fatalf("read printed %q", stdout)
		}
	})

	t.Run("key from a whole link", func(t *testing.T) {
		link := server.URL + "/ui/sealed/nota#k=" + report.Key
		code, stdout, stderr := runCLIWithKey(t, []string{
			"read", "-b", "nota", "-u", server.URL, "--key", link,
		}, "", "")
		if code != exitOK {
			t.Fatalf("exit code %d: %s", code, stderr)
		}
		if stdout != "prima riga" {
			t.Fatalf("read printed %q", stdout)
		}
	})

	t.Run("a flag wins over the environment", func(t *testing.T) {
		code, stdout, _ := runCLIWithKey(t, []string{
			"read", "-b", "nota", "-u", server.URL, "--key", report.Key, "--stat",
		}, "clearly-not-the-key", "")
		if code != exitOK {
			t.Fatalf("exit code %d, want the flag key to win", code)
		}
		if !strings.Contains(stdout, "prima riga") {
			t.Fatalf("read printed %q", stdout)
		}
	})

	t.Run("add seals what it appended", func(t *testing.T) {
		code, _, stderr := runCLIWithKey(t, []string{
			"add", "-b", "nota", "-u", server.URL, "--key", report.Key, "seconda riga",
		}, "", "")
		if code != exitOK {
			t.Fatalf("add exit code %d: %s", code, stderr)
		}

		code, stdout, stderr := runCLIWithKey(t, []string{
			"read", "-b", "nota", "-u", server.URL, "--key", report.Key,
		}, "", "")
		if code != exitOK {
			t.Fatalf("read exit code %d: %s", code, stderr)
		}
		if stdout != "prima riga\nseconda riga" {
			t.Fatalf("read printed %q, want the appended sealed text", stdout)
		}
	})

	t.Run("read --json carries the decrypted text", func(t *testing.T) {
		code, stdout, stderr := runCLIWithKey(t, []string{
			"read", "-b", "nota", "-u", server.URL, "--key", report.Key, "--json",
		}, "", "")
		if code != exitOK {
			t.Fatalf("exit code %d: %s", code, stderr)
		}

		var board struct {
			Mode string `json:"mode"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(stdout), &board); err != nil {
			t.Fatalf("decoding the board: %v", err)
		}
		if board.Mode != "sealed" || !strings.Contains(board.Text, "prima riga") {
			t.Fatalf("unexpected board: %+v", board)
		}
	})
}

func TestSealedBurnBoardIsOpenedOnce(t *testing.T) {
	server := newSealedCLIServer(t)

	code, stdout, stderr := runCLIWithKey(t, []string{
		"sealed", "new", "-b", "effimera", "-u", server.URL, "--json", "una volta sola",
	}, "", "")
	if code != exitOK {
		t.Fatalf("sealed new exit code %d: %s", code, stderr)
	}

	var report struct {
		Key  string `json:"key"`
		Burn bool   `json:"burn"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decoding the report: %v", err)
	}
	if !report.Burn {
		t.Fatal("sealed new did not create a burn board")
	}

	t.Run("a plain read refuses to consume it", func(t *testing.T) {
		code, _, stderr := runCLIWithKey(t, []string{
			"read", "-b", "effimera", "-u", server.URL, "--key", report.Key,
		}, "", "")
		if code != exitError {
			t.Fatalf("exit code %d, want %d: %s", code, exitError, stderr)
		}
		if !strings.Contains(stderr, "open") {
			t.Fatalf("the error does not point at open: %s", stderr)
		}
	})

	t.Run("open prints the text and destroys the board", func(t *testing.T) {
		code, stdout, stderr := runCLIWithKey(t, []string{
			"open", "-b", "effimera", "-u", server.URL, "--key", report.Key,
		}, "", "")
		if code != exitOK {
			t.Fatalf("exit code %d: %s", code, stderr)
		}
		if stdout != "una volta sola" {
			t.Fatalf("open printed %q", stdout)
		}
		if !strings.Contains(stderr, "destroyed") {
			t.Fatalf("open did not say the board is gone: %s", stderr)
		}
	})

	t.Run("the second open is gone", func(t *testing.T) {
		code, _, stderr := runCLIWithKey(t, []string{
			"open", "-b", "effimera", "-u", server.URL, "--key", report.Key,
		}, "", "")
		if code != exitError {
			t.Fatalf("exit code %d, want %d: %s", code, exitError, stderr)
		}
		if !strings.Contains(stderr, "410") {
			t.Fatalf("the error does not report the gone board: %s", stderr)
		}
	})
}

func TestSealedKeyErrors(t *testing.T) {
	server := newSealedCLIServer(t)

	// A board without burn, so "read" reaches the key instead of refusing a burn
	// board first.
	code, stdout, stderr := runCLIWithKey(t, []string{
		"sealed", "new", "-b", "nota", "-u", server.URL, "--json", "--burn=false", "segreto",
	}, "", "")
	if code != exitOK {
		t.Fatalf("sealed new exit code %d: %s", code, stderr)
	}

	var report struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decoding the report: %v", err)
	}

	t.Run("open without a key", func(t *testing.T) {
		code, _, stderr := runCLIWithKey(t, []string{
			"open", "-b", "nota", "-u", server.URL,
		}, "", "")
		if code != exitSealedKey {
			t.Fatalf("exit code %d, want %d: %s", code, exitSealedKey, stderr)
		}
		for _, source := range []string{"--key", "--key-file", "GO_SPIN_BOARD_KEY"} {
			if !strings.Contains(stderr, source) {
				t.Fatalf("the error does not name %s: %s", source, stderr)
			}
		}
	})

	t.Run("a wrong key", func(t *testing.T) {
		other := strings.Repeat("0", 43)
		code, _, stderr := runCLIWithKey(t, []string{
			"read", "-b", "nota", "-u", server.URL, "--key", other,
		}, "", "")
		if code != exitError {
			t.Fatalf("exit code %d, want %d: %s", code, exitError, stderr)
		}
		if !strings.Contains(stderr, "does not open") {
			t.Fatalf("the error does not explain the wrong key: %s", stderr)
		}
	})

	t.Run("an unusable key", func(t *testing.T) {
		code, _, stderr := runCLIWithKey(t, []string{
			"read", "-b", "nota", "-u", server.URL, "--key", "too-short",
		}, "", "")
		if code != exitError {
			t.Fatalf("exit code %d, want %d: %s", code, exitError, stderr)
		}
		if !strings.Contains(stderr, "not a usable key") {
			t.Fatalf("the error does not explain the key form: %s", stderr)
		}
	})

	t.Run("a missing key file", func(t *testing.T) {
		code, _, stderr := runCLIWithKey(t, []string{
			"read", "-b", "nota", "-u", server.URL, "--key-file", filepath.Join(t.TempDir(), "nope"),
		}, "", "")
		if code != exitError {
			t.Fatalf("exit code %d, want %d: %s", code, exitError, stderr)
		}
		if !strings.Contains(stderr, "key file") {
			t.Fatalf("the error does not name the key file: %s", stderr)
		}
	})

	t.Run("sealed new without text", func(t *testing.T) {
		code, _, stderr := runCLIWithKey(t, []string{
			"sealed", "new", "-b", "vuota", "-u", server.URL,
		}, "", "")
		if code != exitUsage {
			t.Fatalf("exit code %d, want %d: %s", code, exitUsage, stderr)
		}
	})

	t.Run("an unknown sealed action", func(t *testing.T) {
		code, _, stderr := runCLIWithKey(t, []string{
			"sealed", "burn", "-b", "nota", "-u", server.URL,
		}, "", "")
		if code != exitUsage {
			t.Fatalf("exit code %d, want %d: %s", code, exitUsage, stderr)
		}
		if !strings.Contains(stderr, "use new") {
			t.Fatalf("the error does not list the actions: %s", stderr)
		}
	})
}

func TestSealedNewWithAnExistingKey(t *testing.T) {
	server := newSealedCLIServer(t)

	code, stdout, stderr := runCLIWithKey(t, []string{
		"sealed", "new", "-b", "nota", "-u", server.URL, "--json", "--burn=false", "primo",
	}, "", "")
	if code != exitOK {
		t.Fatalf("sealed new exit code %d: %s", code, stderr)
	}

	var first struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal([]byte(stdout), &first); err != nil {
		t.Fatalf("decoding the report: %v", err)
	}

	// Creating a second board with the same key keeps every envelope readable
	// with one key.
	code, stdout, stderr = runCLIWithKey(t, []string{
		"sealed", "new", "-b", "altra", "-u", server.URL, "--json", "--burn=false", "--key", first.Key, "secondo",
	}, "", "")
	if code != exitOK {
		t.Fatalf("sealed new exit code %d: %s", code, stderr)
	}

	var second struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal([]byte(stdout), &second); err != nil {
		t.Fatalf("decoding the report: %v", err)
	}
	if second.Key != first.Key {
		t.Fatalf("the given key was replaced: %q, want %q", second.Key, first.Key)
	}
}
