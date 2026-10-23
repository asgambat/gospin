package route

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
			if !strings.Contains(body, "Ghostboard") {
				t.Error("page does not contain the Ghostboard title")
			}
			if !strings.Contains(body, `src="/ui/assets/ghostboard.js?v=`) {
				t.Error("script tag is missing the cache-busting hash")
			}
		})
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
