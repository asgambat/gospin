package route

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/bassista/go_spin/internal/ghostboard"
)

func testGhostboardRouter(t *testing.T) *gin.Engine {
	t.Helper()

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	return SetupRoutes(testApp(), logger)
}

func TestGhostboardRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := testGhostboardRouter(t)

	t.Run("unknown board is empty", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ghostboard/note", nil))

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
		}

		var board ghostboard.Board
		if err := json.Unmarshal(w.Body.Bytes(), &board); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		if board.Name != "note" || board.Text != "" {
			t.Errorf("unexpected board: %+v", board)
		}
	})

	t.Run("raw body update round-trips", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/ghostboard/note", strings.NewReader("riga 1\nriga 2"))
		req.Header.Set("Content-Type", "text/plain")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
		}

		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ghostboard/note", nil))

		var board ghostboard.Board
		if err := json.Unmarshal(w.Body.Bytes(), &board); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		if board.Text != "riga 1\nriga 2" {
			t.Errorf("Text = %q, want the posted text", board.Text)
		}
	})

	t.Run("invalid board name is rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/ghostboard/.hidden", strings.NewReader(`{"text":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
		}
	})
}

func TestGhostboardPageRoutes(t *testing.T) {
	// The page handler reads ui/ghostboard.html relative to the repository root,
	// which is also the process working directory in production.
	t.Chdir("../../..")
	gin.SetMode(gin.TestMode)
	r := testGhostboardRouter(t)

	paths := []string{"/ui/ghostboard", "/ui/ghostboard/lavoro", "/ui/ghostboard/lavoro/"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
			}
			if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Errorf("Content-Type = %q, want text/html", ct)
			}

			body := w.Body.String()
			if !strings.Contains(body, "<title>Ghostboard</title>") {
				t.Error("page does not use the Ghostboard title")
			}
			if !strings.Contains(body, `src="/ui/assets/ghostboard.js?v=`) {
				t.Error("script tag is missing the cache-busting hash")
			}
			for _, icon := range []string{
				`href="/ui/assets/ghostboard-icon.svg?v=`,
				`href="/ui/assets/ghostboard-icon-32.png?v=`,
			} {
				if !strings.Contains(body, icon) {
					t.Errorf("favicon link %q is missing the cache-busting hash", icon)
				}
			}
			if strings.Contains(body, "?v=dev") {
				t.Error("cache-busting hash fell back to dev: an asset file is missing")
			}
			if !strings.Contains(body, `href="/ui/assets/ghostboard-icon-180.png?v=`) {
				t.Error("apple-touch-icon is missing the cache-busting hash")
			}

			maxBytes := testApp().Config.Ghostboard.WithDefaults().MaxTextBytes
			if want := `data-max-bytes="` + strconv.Itoa(maxBytes) + `"`; !strings.Contains(body, want) {
				t.Errorf("page does not carry %s", want)
			}

			// The Open .txt button must not take the focus: only then the picked
			// file lands at the caret instead of always being appended.
			if !strings.Contains(body, `@mousedown.prevent`) {
				t.Error("Open .txt button steals the focus, so the file can only be appended at the end")
			}
		})
	}

	// The rewritten links must point at assets the router actually serves.
	assets := map[string]string{
		"/ui/assets/ghostboard-icon.svg":          "image/svg+xml",
		"/ui/assets/ghostboard-icon-32.png":       "image/png",
		"/ui/assets/ghostboard-icon-180.png":      "image/png",
		"/ui/assets/pwa/ghostboard-icon-192.png":  "image/png",
		"/ui/assets/pwa/ghostboard-icon-512.png":  "image/png",
		"/ui/assets/pwa/manifest-ghostboard.json": "application/manifest+json",
	}
	for path, wantType := range assets {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))

		if w.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, w.Code)
			continue
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, wantType) {
			t.Errorf("GET %s Content-Type = %q, want %s", path, ct, wantType)
		}
		if w.Body.Len() == 0 {
			t.Errorf("GET %s returned an empty body", path)
		}
	}
}

func TestBoardsAPIRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := testGhostboardRouter(t)

	const text = "line one\nline two\n"
	req := httptest.NewRequest(http.MethodPost, "/api/boards/note", strings.NewReader(text))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("POST status = %d, want 204: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/boards/note", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	if got := w.Body.String(); got != text {
		t.Errorf("GET body = %q, want %q", got, text)
	}
}

func TestGhostboardEventStreamIsNotCompressed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := testGhostboardRouter(t)

	server := httptest.NewServer(r)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/ghostboard/note/events", nil)
	if err != nil {
		cancel()
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Accept-Encoding", "gzip")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("GET events error = %v", err)
	}

	if encoding := resp.Header.Get("Content-Encoding"); encoding == "gzip" {
		t.Error("event stream must not be gzip encoded")
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	cancel()
	if err := resp.Body.Close(); err != nil {
		t.Errorf("Body.Close() error = %v", err)
	}
}

func TestGhostboardEventStreamPathDetection(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/ghostboard/note/events", true},
		{"/ghostboard/lavoro/events", true},
		{"/ghostboard/note", false},
		{"/containers/orphans/events", false},
	}

	for _, tt := range tests {
		if got := isGhostboardEventStream(tt.path); got != tt.want {
			t.Errorf("isGhostboardEventStream(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
