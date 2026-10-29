package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/asgambat/gospin/internal/ghostboard"
)

const cliPINSecret = "0123456789abcdef-secret"

// cliPINServer serves one protected board and issues a token on unlock.
type cliPINServer struct {
	tokens map[string]bool
	text   string
	mu     sync.Mutex
}

func newCLIPINServer(t *testing.T) *httptest.Server {
	t.Helper()

	fake := &cliPINServer{tokens: make(map[string]bool), text: "riservato"}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return server
}

func (s *cliPINServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/unlock") {
		var body struct {
			PIN string `json:"pin"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.PIN != "1234" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"invalid pin"}`)
			return
		}

		s.mu.Lock()
		s.tokens["cli-token"] = true
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"token":"cli-token"}`)
		return
	}

	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	s.mu.Lock()
	authorized := s.tokens[token]
	text := s.text
	s.mu.Unlock()

	if !authorized {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"pin required"}`)
		return
	}

	switch {
	case strings.HasPrefix(r.URL.Path, "/api/boards/"):
		_, _ = io.WriteString(w, text)
	case strings.HasPrefix(r.URL.Path, "/ghostboard/"):
		_, _ = fmt.Fprintf(w, `{"name":"lavoro","text":%q,"revision":1}`, text)
	default:
		http.NotFound(w, r)
	}
}

func TestPinHashMatchesTheServer(t *testing.T) {
	t.Setenv("GO_SPIN_GHOSTBOARD_PIN_SECRET", cliPINSecret)

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"--no-config", "pin-hash", "1234"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d (stderr %s)", code, exitOK, stderr.String())
	}

	want, err := ghostboard.PINDigest(cliPINSecret, "1234")
	if err != nil {
		t.Fatalf("PINDigest: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Errorf("pin-hash = %q, want %q", got, want)
	}
}

func TestPinHashReadsThePINFromStdin(t *testing.T) {
	t.Setenv("GO_SPIN_GHOSTBOARD_PIN_SECRET", cliPINSecret)

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"--no-config", "pin-hash"}, strings.NewReader("1234\n"), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d (stderr %s)", code, exitOK, stderr.String())
	}

	want, err := ghostboard.PINDigest(cliPINSecret, "1234")
	if err != nil {
		t.Fatalf("PINDigest: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Errorf("pin-hash = %q, want %q", got, want)
	}
}

func TestPinHashReportsAMissingSecret(t *testing.T) {
	t.Setenv("GO_SPIN_GHOSTBOARD_PIN_SECRET", "")

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"--no-config", "pin-hash", "1234"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitError {
		t.Fatalf("exit code = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr.String(), "GO_SPIN_GHOSTBOARD_PIN_SECRET") {
		t.Errorf("stderr %q does not name the missing secret", stderr.String())
	}
}

func TestPinHashRejectsAMalformedPIN(t *testing.T) {
	t.Setenv("GO_SPIN_GHOSTBOARD_PIN_SECRET", cliPINSecret)

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"--no-config", "pin-hash", "12"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitError {
		t.Fatalf("exit code = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr.String(), "4-64") {
		t.Errorf("stderr %q does not explain the accepted length", stderr.String())
	}
}

func TestConfigNeverPrintsThePIN(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"--no-config", "config", "--pin", "1234"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d (stderr %s)", code, exitOK, stderr.String())
	}

	output := stdout.String()
	if strings.Contains(output, "1234") {
		t.Errorf("config printed the pin: %s", output)
	}
	if !strings.Contains(output, "pin:          set") {
		t.Errorf("config does not report the pin as set: %s", output)
	}
}

func TestReadProtectedBoardWithPIN(t *testing.T) {
	server := newCLIPINServer(t)

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"--no-config", "-u", server.URL, "-b", "lavoro", "--pin", "1234", "read"},
		strings.NewReader(""), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d (stderr %s)", code, exitOK, stderr.String())
	}
	if stdout.String() != "riservato" {
		t.Errorf("stdout = %q, want the board text", stdout.String())
	}
}

func TestReadProtectedBoardWithARejectedPIN(t *testing.T) {
	server := newCLIPINServer(t)

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"--no-config", "-u", server.URL, "-b", "lavoro", "--pin", "9999", "read"},
		strings.NewReader(""), &stdout, &stderr)
	if code != exitError {
		t.Fatalf("exit code = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr.String(), "rejected") {
		t.Errorf("stderr %q does not say the pin was rejected", stderr.String())
	}
}

func TestReadProtectedBoardWithoutAPIN(t *testing.T) {
	server := newCLIPINServer(t)
	t.Setenv("GO_SPIN_BOARD_PIN", "")

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"--no-config", "-u", server.URL, "-b", "lavoro", "read"},
		strings.NewReader(""), &stdout, &stderr)
	if code != exitError {
		t.Fatalf("exit code = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr.String(), "private") {
		t.Errorf("stderr %q does not say the board is private", stderr.String())
	}
}

func TestReadProtectedBoardWithThePINFromTheEnvironment(t *testing.T) {
	server := newCLIPINServer(t)
	t.Setenv("GO_SPIN_BOARD_PIN", "1234")

	var stdout, stderr bytes.Buffer
	code := run("gb-cli", []string{"--no-config", "-u", server.URL, "-b", "lavoro", "read"},
		strings.NewReader(""), &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d (stderr %s)", code, exitOK, stderr.String())
	}
	if stdout.String() != "riservato" {
		t.Errorf("stdout = %q, want the board text", stdout.String())
	}
}
