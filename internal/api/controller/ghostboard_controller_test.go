package controller

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bassista/go_spin/internal/ghostboard"
)

// newGhostboardTestController builds the controller, its router and the store
// behind them, so tests can subscribe to the board events.
func newGhostboardTestController(t *testing.T, maxTextBytes int) (*GhostboardController, *gin.Engine, *ghostboard.Store) {
	t.Helper()

	store, err := ghostboard.New(ghostboard.Config{
		ClearDelay:      time.Minute,
		MaxTextBytes:    maxTextBytes,
		PersistInterval: time.Second,
	})
	if err != nil {
		t.Fatalf("ghostboard.New() error = %v", err)
	}

	gc := NewGhostboardController(store, maxTextBytes, 20*time.Millisecond)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ghostboard/:board", gc.Get)
	r.POST("/ghostboard/:board", gc.Update)
	r.GET("/ghostboard/:board/events", gc.Events)
	r.GET("/api/boards/:board", gc.GetText)
	r.POST("/api/boards/:board", gc.SetText)
	return gc, r, store
}

func newGhostboardTestRouter(t *testing.T, maxTextBytes int) *gin.Engine {
	t.Helper()

	_, r, _ := newGhostboardTestController(t, maxTextBytes)
	return r
}

func TestGhostboardGetUnknownBoard(t *testing.T) {
	r := newGhostboardTestRouter(t, 1024)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ghostboard/note", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var board ghostboard.Board
	if err := json.Unmarshal(w.Body.Bytes(), &board); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if board.Name != "note" || board.Text != "" || board.Revision != 0 {
		t.Errorf("unexpected board: %+v", board)
	}
}

func TestGhostboardUpdateWithJSON(t *testing.T) {
	r := newGhostboardTestRouter(t, 1024)

	body := bytes.NewBufferString(`{"text":"ciao\nmondo"}`)
	req := httptest.NewRequest(http.MethodPost, "/ghostboard/note", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ghostboard-Client", "tab-1")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var board ghostboard.Board
	if err := json.Unmarshal(w.Body.Bytes(), &board); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if board.Text != "ciao\nmondo" {
		t.Errorf("Text = %q, want newline preserved", board.Text)
	}
	if board.Revision != 1 {
		t.Errorf("Revision = %d, want 1", board.Revision)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ghostboard/note", nil))
	if got := w.Body.String(); !strings.Contains(got, `"text":"ciao\nmondo"`) {
		t.Errorf("GET body = %s, want the stored text", got)
	}
}

func TestGhostboardUpdateWithRawBody(t *testing.T) {
	r := newGhostboardTestRouter(t, 1024)

	req := httptest.NewRequest(http.MethodPost, "/ghostboard/note", strings.NewReader("<b>raw</b>\nsecond line"))
	req.Header.Set("Content-Type", "text/plain")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var board ghostboard.Board
	if err := json.Unmarshal(w.Body.Bytes(), &board); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if board.Text != "<b>raw</b>\nsecond line" {
		t.Errorf("Text = %q, want the raw body untouched", board.Text)
	}
}

func TestGhostboardUpdateWithFormBody(t *testing.T) {
	r := newGhostboardTestRouter(t, 1024)

	form := url.Values{"text": {"valore con & e +"}}
	req := httptest.NewRequest(http.MethodPost, "/ghostboard/note", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var board ghostboard.Board
	if err := json.Unmarshal(w.Body.Bytes(), &board); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if board.Text != "valore con & e +" {
		t.Errorf("Text = %q, want the decoded form value", board.Text)
	}
}

func TestGhostboardUpdateWithFormBodyWithoutTextField(t *testing.T) {
	r := newGhostboardTestRouter(t, 1024)

	req := httptest.NewRequest(http.MethodPost, "/ghostboard/note", strings.NewReader("plain body from curl -d"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var board ghostboard.Board
	if err := json.Unmarshal(w.Body.Bytes(), &board); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if board.Text != "plain body from curl -d" {
		t.Errorf("Text = %q, want the raw body when no text field is present", board.Text)
	}
}

func TestGhostboardUpdateClearsBoard(t *testing.T) {
	r := newGhostboardTestRouter(t, 1024)

	req := httptest.NewRequest(http.MethodPost, "/ghostboard/note", strings.NewReader(`{"text":"testo"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(httptest.NewRecorder(), req)

	req = httptest.NewRequest(http.MethodPost, "/ghostboard/note", strings.NewReader(`{"text":""}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var board ghostboard.Board
	if err := json.Unmarshal(w.Body.Bytes(), &board); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if board.Text != "" || board.Revision != 2 {
		t.Errorf("board = %+v, want empty text at revision 2", board)
	}
}

func TestGhostboardUpdateRejectsInvalidBoardName(t *testing.T) {
	r := newGhostboardTestRouter(t, 1024)

	req := httptest.NewRequest(http.MethodPost, "/ghostboard/.hidden", strings.NewReader(`{"text":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestGhostboardUpdateRejectsInvalidJSON(t *testing.T) {
	r := newGhostboardTestRouter(t, 1024)

	req := httptest.NewRequest(http.MethodPost, "/ghostboard/note", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestGhostboardUpdateRejectsOversizedText(t *testing.T) {
	r := newGhostboardTestRouter(t, 8)

	req := httptest.NewRequest(http.MethodPost, "/ghostboard/note", strings.NewReader(`{"text":"0123456789"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413: %s", w.Code, w.Body.String())
	}
}

func TestGhostboardUpdateRejectsOversizedRawBody(t *testing.T) {
	r := newGhostboardTestRouter(t, 8)

	req := httptest.NewRequest(http.MethodPost, "/ghostboard/note", strings.NewReader(strings.Repeat("x", 4096)))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413: %s", w.Code, w.Body.String())
	}
}

// TestGhostboardUpdateRejectsInvalidUTF8 pins the persistence invariant on the
// legacy endpoint too: a body that is not valid UTF-8 would be rewritten with
// U+FFFD when the store saves the board, silently changing the text.
func TestGhostboardUpdateRejectsInvalidUTF8(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "raw body", contentType: "text/plain", body: "bad \xff\xfe"},
		{name: "form field", contentType: "application/x-www-form-urlencoded", body: "text=bad+%FF%FE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newGhostboardTestRouter(t, 1024)

			req := httptest.NewRequest(http.MethodPost, "/ghostboard/note", strings.NewReader("valid"))
			req.Header.Set("Content-Type", "text/plain")
			r.ServeHTTP(httptest.NewRecorder(), req)

			req = httptest.NewRequest(http.MethodPost, "/ghostboard/note", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
			}

			w = httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ghostboard/note", nil))

			var board ghostboard.Board
			if err := json.Unmarshal(w.Body.Bytes(), &board); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if board.Text != "valid" {
				t.Errorf("Text = %q, want the rejected write to leave the board untouched", board.Text)
			}
		})
	}
}

func TestBoardsAPIGetUnknownBoardIsEmpty(t *testing.T) {
	r := newGhostboardTestRouter(t, 1024)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/boards/note", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	if w.Body.Len() != 0 {
		t.Errorf("body = %q, want an empty body", w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

// TestBoardsAPISetStoresRawBody pins the CLI contract: every body is stored
// byte for byte, whatever the Content-Type claims, newlines included.
func TestBoardsAPISetStoresRawBody(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "plain text", contentType: "text/plain", body: "# title\nsecond line\n"},
		{name: "json body is not decoded", contentType: "application/json", body: `{"text":"hello"}`},
		{name: "form body is not decoded", contentType: "application/x-www-form-urlencoded", body: "text=hello+world"},
		{name: "trailing newline is kept", contentType: "text/plain", body: "echoed\n"},
		{name: "multibyte UTF-8", contentType: "text/plain; charset=utf-8", body: "caffè 😀\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newGhostboardTestRouter(t, 1024)

			req := httptest.NewRequest(http.MethodPost, "/api/boards/note", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want 204: %s", w.Code, w.Body.String())
			}
			if w.Body.Len() != 0 {
				t.Errorf("body = %q, want no body on 204", w.Body.String())
			}

			w = httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/boards/note", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("GET status = %d, want 200: %s", w.Code, w.Body.String())
			}
			if got := w.Body.String(); got != tt.body {
				t.Errorf("GET body = %q, want %q", got, tt.body)
			}
		})
	}
}

func TestBoardsAPISetClearsBoardWithEmptyBody(t *testing.T) {
	r := newGhostboardTestRouter(t, 1024)

	req := httptest.NewRequest(http.MethodPost, "/api/boards/note", strings.NewReader("keep me"))
	req.Header.Set("Content-Type", "text/plain")
	r.ServeHTTP(httptest.NewRecorder(), req)

	req = httptest.NewRequest(http.MethodPost, "/api/boards/note", http.NoBody)
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/boards/note", nil))
	if got := w.Body.String(); got != "" {
		t.Errorf("GET body = %q, want the board cleared", got)
	}
}

func TestBoardsAPIRejectsInvalidUTF8(t *testing.T) {
	r := newGhostboardTestRouter(t, 1024)

	req := httptest.NewRequest(http.MethodPost, "/api/boards/note", strings.NewReader("valid"))
	req.Header.Set("Content-Type", "text/plain")
	r.ServeHTTP(httptest.NewRecorder(), req)

	req = httptest.NewRequest(http.MethodPost, "/api/boards/note", strings.NewReader("bad \xff\xfe"))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}

	// The rejected body must leave the board untouched.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/boards/note", nil))
	if got := w.Body.String(); got != "valid" {
		t.Errorf("GET body = %q, want the previous text", got)
	}
}

func TestBoardsAPISizeLimit(t *testing.T) {
	const maxTextBytes = 8

	tests := []struct {
		name string
		body string
		want int
	}{
		{name: "at the limit", body: strings.Repeat("x", maxTextBytes), want: http.StatusNoContent},
		{name: "over the limit", body: strings.Repeat("x", maxTextBytes+1), want: http.StatusRequestEntityTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newGhostboardTestRouter(t, maxTextBytes)

			req := httptest.NewRequest(http.MethodPost, "/api/boards/note", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "text/plain")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, tt.want, w.Body.String())
			}
		})
	}
}

func TestBoardsAPIRejectsOversizedBodyWithoutChangingBoard(t *testing.T) {
	r := newGhostboardTestRouter(t, 8)

	req := httptest.NewRequest(http.MethodPost, "/api/boards/note", strings.NewReader("keep"))
	req.Header.Set("Content-Type", "text/plain")
	r.ServeHTTP(httptest.NewRecorder(), req)

	req = httptest.NewRequest(http.MethodPost, "/api/boards/note", strings.NewReader(strings.Repeat("x", 9)))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/boards/note", nil))
	if got := w.Body.String(); got != "keep" {
		t.Errorf("GET body = %q, want the rejected write to leave the board untouched", got)
	}
}

func TestBoardsAPIPassesClientID(t *testing.T) {
	_, r, store := newGhostboardTestController(t, 1024)

	sub, _, err := store.Subscribe("note")
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer sub.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/boards/note", strings.NewReader("x"))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("X-Ghostboard-Client", "cli-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", w.Code, w.Body.String())
	}

	select {
	case event := <-sub.Events():
		if event.Text != "x" {
			t.Errorf("event.Text = %q, want x", event.Text)
		}
		if event.ClientID != "cli-1" {
			t.Errorf("event.ClientID = %q, want the header value", event.ClientID)
		}
	case <-time.After(time.Second):
		t.Fatal("no event received for the CLI write")
	}
}

func TestBoardsAPIRejectsInvalidBoardName(t *testing.T) {
	r := newGhostboardTestRouter(t, 1024)

	methods := []string{http.MethodGet, http.MethodPost}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/boards/.hidden", strings.NewReader("x"))
			req.Header.Set("Content-Type", "text/plain")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestGhostboardEventsStreamSnapshotAndUpdate(t *testing.T) {
	r := newGhostboardTestRouter(t, 1024)
	server := httptest.NewServer(r)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/ghostboard/note/events", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET events error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	scanner := bufio.NewScanner(resp.Body)
	readEvent := func() ghostboard.Event {
		t.Helper()

		var data string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				data = strings.TrimPrefix(line, "data: ")
			}
			if line == "" && data != "" {
				var event ghostboard.Event
				if err := json.Unmarshal([]byte(data), &event); err != nil {
					t.Fatalf("Unmarshal(%q) error = %v", data, err)
				}
				return event
			}
		}
		t.Fatalf("stream ended before an event was received: %v", scanner.Err())
		return ghostboard.Event{}
	}

	snapshot := readEvent()
	if snapshot.Name != "note" || snapshot.Text != "" || snapshot.Revision != 0 {
		t.Errorf("snapshot = %+v, want the empty initial board", snapshot)
	}

	body := bytes.NewBufferString(`{"text":"live"}`)
	updateReq, err := http.NewRequest(http.MethodPost, server.URL+"/ghostboard/note", body)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.Header.Set("X-Ghostboard-Client", "tab-42")
	updateResp, err := http.DefaultClient.Do(updateReq)
	if err != nil {
		t.Fatalf("POST error = %v", err)
	}
	defer updateResp.Body.Close()
	if updateResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(updateResp.Body)
		t.Fatalf("POST status = %d: %s", updateResp.StatusCode, raw)
	}

	update := readEvent()
	if update.Text != "live" || update.Revision != 1 {
		t.Errorf("update event = %+v, want text live at revision 1", update)
	}
	if update.ClientID != "tab-42" {
		t.Errorf("ClientID = %q, want tab-42", update.ClientID)
	}
}

func TestGhostboardEventsRejectsInvalidBoard(t *testing.T) {
	r := newGhostboardTestRouter(t, 1024)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ghostboard/.hidden/events", nil))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
}

// TestGhostboardUpdateAcceptsEscapedTextWithinLimit pins the body-size contract:
// the limit applies to the decoded text, so a payload whose encoding expands it
// (JSON \u003c escapes, percent-encoded forms) is accepted as long as the text
// itself fits max_text_bytes.
func TestGhostboardUpdateAcceptsEscapedTextWithinLimit(t *testing.T) {
	const maxTextBytes = 1000
	text := strings.Repeat("<", maxTextBytes)

	jsonBody, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	formBody := url.Values{"text": {text}}.Encode()

	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "json escapes", contentType: "application/json", body: string(jsonBody)},
		{name: "percent-encoded form", contentType: "application/x-www-form-urlencoded", body: formBody},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.body) <= maxTextBytes+ghostboardBodyOverhead {
				t.Fatalf("payload of %d bytes does not exercise the escape expansion", len(tt.body))
			}

			r := newGhostboardTestRouter(t, maxTextBytes)
			req := httptest.NewRequest(http.MethodPost, "/ghostboard/note", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
			}

			var board ghostboard.Board
			if err := json.Unmarshal(w.Body.Bytes(), &board); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if board.Text != text {
				t.Errorf("len(Text) = %d, want %d", len(board.Text), maxTextBytes)
			}
		})
	}
}
