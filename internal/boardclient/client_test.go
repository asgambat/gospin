package boardclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeServer is a minimal stand-in for the GoSpin plain-text board API.
type fakeServer struct {
	boards map[string]string
	mu     sync.Mutex
	limit  int
	calls  int
}

func newFakeServer(t *testing.T) (*fakeServer, *httptest.Server) {
	t.Helper()

	fake := &fakeServer{boards: make(map[string]string), limit: 1 << 20}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	return fake, server
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()

	name := strings.TrimPrefix(r.URL.Path, "/api/boards/")
	if name == r.URL.Path || name == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		f.mu.Lock()
		text := f.boards[name]
		f.mu.Unlock()

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if _, err := io.WriteString(w, text); err != nil {
			return
		}
	case http.MethodPost:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "cannot read body", http.StatusBadRequest)
			return
		}
		if len(body) > f.limit {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			if err := json.NewEncoder(w).Encode(map[string]string{"error": "text exceeds the maximum allowed size"}); err != nil {
				return
			}
			return
		}

		f.mu.Lock()
		f.boards[name] = string(body)
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (f *fakeServer) text(board string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.boards[board]
}

func (f *fakeServer) set(board, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.boards[board] = text
}

func (f *fakeServer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "plain", input: "http://localhost:8084", want: "http://localhost:8084"},
		{name: "trailing slash", input: "http://localhost:8084/", want: "http://localhost:8084"},
		{name: "surrounding spaces", input: "  http://localhost:8084  ", want: "http://localhost:8084"},
		{name: "path prefix", input: "https://example.com/gospin/", want: "https://example.com/gospin"},
		{name: "empty", input: "", wantErr: true},
		{name: "missing scheme", input: "localhost:8084", wantErr: true},
		{name: "unsupported scheme", input: "ftp://example.com", wantErr: true},
		{name: "missing host", input: "http://", wantErr: true},
		{name: "query", input: "http://example.com/?a=1", wantErr: true},
		{name: "fragment", input: "http://example.com/#top", wantErr: true},
		{name: "dashboard page", input: "https://gospin.lan.asga.uk/ui/dashboard/", wantErr: true},
		{name: "ui root", input: "https://example.com/ui", wantErr: true},
		{name: "prefix with ui in the name", input: "https://example.com/uint", want: "https://example.com/uint"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeURL(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeURLRejectsADashboardPage(t *testing.T) {
	_, err := NormalizeURL("https://gospin.lan.asga.uk/ui/dashboard/")
	if err == nil {
		t.Fatal("expected an error for a UI page URL")
	}

	for _, want := range []string{"/ui/dashboard/", "GoSpin page", "https://gospin.lan.asga.uk"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

func TestNewRejectsNonPositiveTimeout(t *testing.T) {
	if _, err := New("http://localhost:8084", 0); err == nil {
		t.Fatal("expected an error for a zero timeout")
	}
}

func TestGet(t *testing.T) {
	fake, server := newFakeServer(t)
	fake.set("lavoro", "hello\nworld")

	client, err := New(server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text, err := client.Get(context.Background(), "lavoro")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hello\nworld" {
		t.Fatalf("got %q, want %q", text, "hello\nworld")
	}
}

func TestGetUnknownBoardIsEmpty(t *testing.T) {
	_, server := newFakeServer(t)

	client, err := New(server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text, err := client.Get(context.Background(), "missing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "" {
		t.Fatalf("got %q, want an empty text", text)
	}
}

func TestSet(t *testing.T) {
	fake, server := newFakeServer(t)

	client, err := New(server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := client.Set(context.Background(), "note", "  trailing newline\n"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := fake.text("note"); got != "  trailing newline\n" {
		t.Fatalf("stored %q, want the text byte for byte", got)
	}

	if err := client.Set(context.Background(), "note", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := fake.text("note"); got != "" {
		t.Fatalf("stored %q, want the board cleared", got)
	}
}

func TestAppend(t *testing.T) {
	tests := []struct {
		name    string
		current string
		text    string
		want    string
	}{
		{
			name:    "newline when the board has no trailing one",
			current: "prima",
			text:    "dopo",
			want:    "prima\ndopo",
		},
		{
			name:    "no double newline when the board ends with one",
			current: "prima\n",
			text:    "dopo\n",
			want:    "prima\ndopo\n",
		},
		{
			name:    "no leading newline on an empty board",
			current: "",
			text:    "dopo",
			want:    "dopo",
		},
		{
			name:    "no double newline when the text starts with one",
			current: "prima",
			text:    "\ndopo",
			want:    "prima\ndopo",
		},
		{
			name:    "empty addition changes nothing",
			current: "prima",
			text:    "",
			want:    "prima",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake, server := newFakeServer(t)
			fake.set("lavoro", test.current)

			client, err := New(server.URL, 5*time.Second)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			updated, err := client.Append(context.Background(), "lavoro", test.text)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if updated != test.want {
				t.Fatalf("returned %q, want %q", updated, test.want)
			}
			if got := fake.text("lavoro"); got != test.want {
				t.Fatalf("stored %q, want %q", got, test.want)
			}
		})
	}
}

func TestFollowStreamsSnapshotAndChanges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ghostboard/lavoro/events" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")

		frames := []string{
			"event: board\ndata: {\"name\":\"lavoro\",\"text\":\"uno\",\"revision\":1}\n\n",
			": keep-alive\n\n",
			"event: board\ndata: {\"name\":\"lavoro\",\"text\":\"uno\\ndue\",\"revision\":2}\n\n",
		}
		for _, frame := range frames {
			if _, err := io.WriteString(w, frame); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	client, err := New(server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var states []Board
	if err := client.Follow(context.Background(), "lavoro", func(board Board) {
		states = append(states, board)
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(states) != 2 {
		t.Fatalf("received %d revision(s), want 2: %+v", len(states), states)
	}
	if states[0].Text != "uno" || states[0].Revision != 1 {
		t.Fatalf("first revision %+v", states[0])
	}
	if states[1].Text != "uno\ndue" || states[1].Revision != 2 {
		t.Fatalf("second revision %+v", states[1])
	}
}

func TestFollowReadsAnEventLargerThanTheDefaultBuffer(t *testing.T) {
	// A board is bounded by ghostboard.max_text_bytes, not by a buffer of the
	// CLI: an event well past a few hundred kilobytes must still stream.
	text := strings.Repeat("riga di una board grande\n", 20000) // ~480 KB
	encoded, err := json.Marshal(map[string]any{
		"name":     "grande",
		"text":     text,
		"revision": 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ghostboard/grande/events" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		if _, err := fmt.Fprintf(w, "event: board\ndata: %s\n\n", encoded); err != nil {
			return
		}
	}))
	t.Cleanup(server.Close)

	client, err := New(server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var states []Board
	if err := client.Follow(context.Background(), "grande", func(board Board) {
		states = append(states, board)
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(states) != 1 {
		t.Fatalf("received %d revision(s), want 1", len(states))
	}
	if states[0].Text != text {
		t.Fatalf("streamed %d byte(s), want the %d of the board", len(states[0].Text), len(text))
	}
}

func TestFollowStopsWhenTheContextIsCancelled(t *testing.T) {
	connected := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if _, err := io.WriteString(w, "event: board\ndata: {\"name\":\"note\",\"text\":\"ciao\",\"revision\":1}\n\n"); err != nil {
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(connected)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	client, err := New(server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	snapshots := make(chan Board, 1)
	go func() {
		done <- client.Follow(ctx, "note", func(board Board) {
			select {
			case snapshots <- board:
			default:
			}
		})
	}()

	select {
	case <-connected:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream was never opened")
	}
	select {
	case board := <-snapshots:
		if board.Text != "ciao" {
			t.Fatalf("snapshot %+v", board)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the snapshot was never delivered")
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cancellation reported an error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not stop after cancellation")
	}
}

func TestFollowRejectsInvalidBoardName(t *testing.T) {
	fake, server := newFakeServer(t)

	client, err := New(server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := client.Follow(context.Background(), "bad/name", func(Board) {}); err == nil {
		t.Fatal("expected an error for an invalid board name")
	}
	if fake.callCount() != 0 {
		t.Fatalf("the server was called %d time(s), want 0", fake.callCount())
	}
}

func TestFollowReportsAServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		if _, err := io.WriteString(w, `{"error":"invalid board name"}`); err != nil {
			return
		}
	}))
	t.Cleanup(server.Close)

	client, err := New(server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = client.Follow(context.Background(), "note", func(Board) {})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "following board") || !strings.Contains(err.Error(), "invalid board name") {
		t.Fatalf("error %q does not describe the failure", err)
	}
}

func TestInvalidBoardNameNeverReachesTheServer(t *testing.T) {
	fake, server := newFakeServer(t)

	client, err := New(server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := client.Get(context.Background(), "bad/name"); err == nil {
		t.Fatal("expected an error for an invalid board name")
	}
	if fake.callCount() != 0 {
		t.Fatalf("the server was called %d time(s), want 0", fake.callCount())
	}
}

func TestResponseErrorCarriesTheServerMessage(t *testing.T) {
	fake, server := newFakeServer(t)
	fake.limit = 4

	client, err := New(server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = client.Set(context.Background(), "note", "far too long")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "413") {
		t.Fatalf("error %q does not mention the status", err)
	}
	if !strings.Contains(err.Error(), "text exceeds the maximum allowed size") {
		t.Fatalf("error %q does not carry the server message", err)
	}

	var responseErr *ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("error %q is not a ResponseError", err)
	}
	if responseErr.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", responseErr.Status)
	}
}

func TestNotFoundExplainsTheURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		if _, err := io.WriteString(w, "<html><body>404 page not found</body></html>"); err != nil {
			return
		}
	}))
	t.Cleanup(server.Close)

	client, err := New(server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = client.Get(context.Background(), "note")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "does not expose /api/boards/:board") {
		t.Fatalf("error %q does not point at the URL", err)
	}
	if !strings.Contains(err.Error(), server.URL) {
		t.Fatalf("error %q does not name the server", err)
	}
	if strings.Contains(err.Error(), "page not found") || strings.Contains(err.Error(), "<html>") {
		t.Fatalf("error %q quotes the web page", err)
	}
}

func TestFollowNotFoundExplainsTheURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	client, err := New(server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = client.Follow(context.Background(), "note", func(Board) {})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "does not expose /ghostboard/:board/events") {
		t.Fatalf("error %q does not point at the URL", err)
	}
}

func TestResponseErrorSkipsAnHTMLBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		if _, err := io.WriteString(w, "<html><body>gateway exploded</body></html>"); err != nil {
			return
		}
	}))
	t.Cleanup(server.Close)

	client, err := New(server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = client.Get(context.Background(), "note")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "500 Internal Server Error") {
		t.Fatalf("error %q does not carry the status", err)
	}
	if strings.Contains(err.Error(), "gateway exploded") || strings.Contains(err.Error(), "<html>") {
		t.Fatalf("error %q quotes the HTML page", err)
	}
}

func TestResponseErrorQuotesAPlainBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	client, err := New(server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = client.Get(context.Background(), "note")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error %q does not quote the response body", err)
	}
}

func TestUnreachableServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()

	client, err := New(url, 2*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := client.Get(context.Background(), "note"); err == nil {
		t.Fatal("expected an error for an unreachable server")
	}
}

func TestAppendText(t *testing.T) {
	tests := []struct {
		name    string
		current string
		text    string
		want    string
	}{
		{name: "separator when needed", current: "prima", text: "dopo", want: "prima\ndopo"},
		{name: "no separator when the board ends with a newline", current: "prima\n", text: "dopo", want: "prima\ndopo"},
		{name: "no separator on an empty board", current: "", text: "dopo", want: "dopo"},
		{name: "no separator when the text starts with a newline", current: "prima", text: "\ndopo", want: "prima\ndopo"},
		{name: "empty addition", current: "prima", text: "", want: "prima"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := AppendText(test.current, test.text); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestGetBoard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ghostboard/lavoro" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"name":       "lavoro",
			"text":       "contenuto",
			"revision":   7,
			"updated_at": "2026-10-25T10:00:00Z",
		}); err != nil {
			return
		}
	}))
	t.Cleanup(server.Close)

	client, err := New(server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	board, err := client.GetBoard(context.Background(), "lavoro")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if board.Name != "lavoro" || board.Text != "contenuto" || board.Revision != 7 {
		t.Fatalf("board %+v", board)
	}
	if board.UpdatedAt == nil || board.UpdatedAt.UTC().Format(time.RFC3339) != "2026-10-25T10:00:00Z" {
		t.Fatalf("updated at %v", board.UpdatedAt)
	}
}

func TestGetBoardDropsTheZeroUpdateTime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"name":       "nuova",
			"text":       "",
			"revision":   0,
			"updated_at": "0001-01-01T00:00:00Z",
		}); err != nil {
			return
		}
	}))
	t.Cleanup(server.Close)

	client, err := New(server.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	board, err := client.GetBoard(context.Background(), "nuova")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if board.UpdatedAt != nil {
		t.Fatalf("updated at %v, want no update time for a board that was never written", board.UpdatedAt)
	}

	encoded, err := json.Marshal(board)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(string(encoded), "updated_at") {
		t.Fatalf("encoded board %s still carries a year-1 timestamp", encoded)
	}
}

func TestRetriesTransientFailures(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if _, err := io.WriteString(w, "ok"); err != nil {
			return
		}
	}))
	t.Cleanup(server.Close)

	client, err := NewWithOptions(server.URL, Options{
		Timeout:    5 * time.Second,
		Retries:    2,
		RetryDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text, err := client.Get(context.Background(), "note")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "ok" {
		t.Fatalf("got %q, want %q", text, "ok")
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("the server was called %d time(s), want 3", got)
	}
}

func TestRetriesStopWhenExhausted(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	client, err := NewWithOptions(server.URL, Options{
		Timeout:    5 * time.Second,
		Retries:    1,
		RetryDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := client.Get(context.Background(), "note"); err == nil {
		t.Fatal("expected an error")
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("the server was called %d time(s), want 2", got)
	}
}

func TestNoRetryOnAMissingEndpoint(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.NotFound(w, nil)
	}))
	t.Cleanup(server.Close)

	client, err := NewWithOptions(server.URL, Options{
		Timeout:    5 * time.Second,
		Retries:    3,
		RetryDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = client.Get(context.Background(), "note")
	if err == nil {
		t.Fatal("expected an error")
	}
	var endpointErr *EndpointError
	if !errors.As(err, &endpointErr) {
		t.Fatalf("error %q is not an EndpointError", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("the server was called %d time(s), want 1", got)
	}
}

func TestSendsConfiguredHeaders(t *testing.T) {
	var header http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	client, err := NewWithOptions(server.URL, Options{
		Timeout: 5 * time.Second,
		Headers: http.Header{
			"X-Debug":       []string{"uno"},
			"Authorization": []string{"Bearer segreto"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := client.Set(context.Background(), "note", "testo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := header.Get("X-Debug"); got != "uno" {
		t.Fatalf("X-Debug %q", got)
	}
	if got := header.Get("Authorization"); got != "Bearer segreto" {
		t.Fatalf("Authorization %q", got)
	}
}
