package boardclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// pinServer mimics a GoSpin server whose "lavoro" board is protected by a PIN:
// the board routes answer 401 until the client trades the PIN for a token.
type pinServer struct {
	boards            map[string]string
	tokens            map[string]bool
	pin               string
	proxyToken        string
	lastAuthorization string
	mu                sync.Mutex
	unlocks           int
}

func newPINServer(t *testing.T, pin string) (*pinServer, *httptest.Server) {
	t.Helper()

	fake := &pinServer{
		boards: map[string]string{"lavoro": "riservato"},
		tokens: make(map[string]bool),
		pin:    pin,
	}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, server
}

func (p *pinServer) unlockCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.unlocks
}

func (p *pinServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.lastAuthorization = r.Header.Get("Authorization")
	p.mu.Unlock()

	const unlockSuffix = "/unlock"
	if strings.HasSuffix(r.URL.Path, unlockSuffix) && r.Method == http.MethodPost {
		p.serveUnlock(w, r)
		return
	}

	if !p.authorized(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"pin required"}`)
		return
	}

	switch {
	case strings.HasPrefix(r.URL.Path, "/api/boards/"):
		p.serveText(w, r, strings.TrimPrefix(r.URL.Path, "/api/boards/"))
	case strings.HasSuffix(r.URL.Path, "/events"):
		p.serveEvents(w, strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/ghostboard/"), "/events"))
	default:
		http.NotFound(w, r)
	}
}

func (p *pinServer) serveUnlock(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PIN string `json:"pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "cannot decode the body", http.StatusBadRequest)
		return
	}
	if body.PIN != p.pin {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"invalid pin"}`)
		return
	}

	p.mu.Lock()
	p.unlocks++
	token := fmt.Sprintf("token-%d", p.unlocks)
	p.tokens[token] = true
	p.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": token})
}

func (p *pinServer) authorized(r *http.Request) bool {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")

	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tokens[token] || (p.proxyToken != "" && token == p.proxyToken)
}

func (p *pinServer) serveText(w http.ResponseWriter, r *http.Request, board string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch r.Method {
	case http.MethodGet:
		_, _ = io.WriteString(w, p.boards[board])
	case http.MethodPost:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "cannot read the body", http.StatusBadRequest)
			return
		}
		p.boards[board] = string(body)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *pinServer) serveEvents(w http.ResponseWriter, board string) {
	w.Header().Set("Content-Type", "text/event-stream")

	p.mu.Lock()
	state, err := json.Marshal(map[string]any{
		"name":     board,
		"text":     p.boards[board],
		"revision": 1,
	})
	p.mu.Unlock()
	if err != nil {
		http.Error(w, "cannot encode the event", http.StatusInternalServerError)
		return
	}

	_, _ = fmt.Fprintf(w, "event: board\ndata: %s\n\n", state)
}

func newPINClient(t *testing.T, server *httptest.Server, pin string) *Client {
	t.Helper()

	client, err := NewWithOptions(server.URL, Options{Timeout: 2 * time.Second, PIN: pin})
	if err != nil {
		t.Fatalf("NewWithOptions() error = %v", err)
	}
	return client
}

func TestClientPINUnlocksOncePerCommand(t *testing.T) {
	fake, server := newPINServer(t, "1234")
	client := newPINClient(t, server, "1234")
	ctx := context.Background()

	text, err := client.Get(ctx, "lavoro")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if text != "riservato" {
		t.Errorf("Get() = %q, want \"riservato\"", text)
	}

	if err := client.Set(ctx, "lavoro", "nuovo testo"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if got, _ := client.Get(ctx, "lavoro"); got != "nuovo testo" {
		t.Errorf("Get() after Set() = %q, want \"nuovo testo\"", got)
	}
	if got := fake.unlockCount(); got != 1 {
		t.Errorf("unlock calls = %d, want 1: the token is reused by the client", got)
	}
}

func TestClientPINFollowsAProtectedBoard(t *testing.T) {
	_, server := newPINServer(t, "1234")
	client := newPINClient(t, server, "1234")

	var got Board
	err := client.Follow(context.Background(), "lavoro", func(board Board) { got = board })
	if err != nil {
		t.Fatalf("Follow() error = %v", err)
	}
	if got.Text != "riservato" {
		t.Errorf("Text = %q, want the snapshot of the board", got.Text)
	}
}

func TestClientPINWrapsARejectedPIN(t *testing.T) {
	_, server := newPINServer(t, "1234")
	client := newPINClient(t, server, "9999")

	_, err := client.Get(context.Background(), "lavoro")
	var pinErr *PINError
	if !errors.As(err, &pinErr) {
		t.Fatalf("Get() error = %v, want PINError", err)
	}
	if !pinErr.Rejected {
		t.Error("Rejected = false, want true: the PIN was sent and refused")
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Errorf("error %q does not say the pin was rejected", err)
	}
}

func TestClientPINWithoutAPIN(t *testing.T) {
	_, server := newPINServer(t, "1234")
	client := newPINClient(t, server, "")

	_, err := client.Get(context.Background(), "lavoro")
	var pinErr *PINError
	if !errors.As(err, &pinErr) {
		t.Fatalf("Get() error = %v, want PINError", err)
	}
	if pinErr.Rejected {
		t.Error("Rejected = true, want false: no PIN was sent")
	}
	for _, want := range []string{"--pin", "GO_SPIN_BOARD_PIN"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestLoadPIN(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ConfigFileName)
	if err := os.WriteFile(path, []byte("url: http://board.example.com\npin: \"5555\"\n"), 0o600); err != nil {
		t.Fatalf("writing the config file: %v", err)
	}

	cfg, err := Load(LoadOptions{
		Getenv:     func(string) string { return "" },
		ConfigPath: path,
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.PIN != "5555" {
		t.Errorf("PIN = %q, want 5555 from the configuration file", cfg.PIN)
	}

	cfg, err = Load(LoadOptions{
		Getenv: func(key string) string {
			if key == EnvPIN {
				return "1234"
			}
			return ""
		},
		ConfigPath: path,
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.PIN != "1234" {
		t.Errorf("PIN = %q, want 1234 from %s", cfg.PIN, EnvPIN)
	}

	if err := (Config{URL: "http://localhost", Board: "lavoro", Timeout: time.Second, PIN: "12"}).Validate(); err == nil {
		t.Error("Validate() accepted a pin that is too short")
	}
}

func TestClientPINKeepsAProxyAuthorizationHeader(t *testing.T) {
	fake, server := newPINServer(t, "1234")
	fake.proxyToken = "proxy-secret"

	headers := make(http.Header)
	headers.Set("Authorization", "Bearer proxy-secret")
	client, err := NewWithOptions(server.URL, Options{
		Timeout: 2 * time.Second,
		Headers: headers,
		PIN:     "1234",
	})
	if err != nil {
		t.Fatalf("NewWithOptions() error = %v", err)
	}

	// The caller's Authorization header wins: the credential of a reverse proxy
	// must not be replaced by a board session.
	if _, err := client.Get(context.Background(), "lavoro"); err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	fake.mu.Lock()
	got := fake.lastAuthorization
	unlocks := fake.unlocks
	fake.mu.Unlock()

	if got != "Bearer proxy-secret" {
		t.Errorf("Authorization = %q, want the header of the caller", got)
	}
	if unlocks != 0 {
		t.Errorf("unlock calls = %d, want 0: the board accepted the proxy credential", unlocks)
	}
}
