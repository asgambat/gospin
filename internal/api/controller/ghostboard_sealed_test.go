package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/asgambat/gospin/internal/ghostboard"
	"github.com/asgambat/gospin/internal/sealedcrypto"
)

// newSealedTestRouter builds a controller over a store with sealed boards on. The
// routes mirror the ones ghostboard_route.go registers.
func newSealedTestRouter(t *testing.T) (*gin.Engine, *ghostboard.Store) {
	t.Helper()

	store, err := ghostboard.New(ghostboard.Config{
		ClearDelay:      time.Minute,
		SealedTombstone: time.Hour,
		MaxTextBytes:    4096,
		PersistInterval: time.Second,
		SealedEnabled:   true,
	})
	if err != nil {
		t.Fatalf("ghostboard.New() error = %v", err)
	}

	gc := NewGhostboardController(store, nil, 4096, 20*time.Millisecond)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ghostboard/:board", gc.Get)
	r.POST("/ghostboard/:board", gc.Update)
	r.GET("/ghostboard/:board/events", gc.Events)
	r.GET("/api/boards/:board", gc.GetText)
	r.POST("/api/boards/:board", gc.SetText)
	r.POST("/ghostboard/:board/open", gc.Open)
	r.POST("/api/boards/:board/open", gc.OpenText)
	return r, store
}

// controllerEnvelope seals a text for a board.
func controllerEnvelope(t *testing.T, board, text string) string {
	t.Helper()

	key, err := sealedcrypto.NewKey()
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	envelope, err := sealedcrypto.Seal(key, board, []byte(text))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	return envelope
}

// writeWithHeaders posts a raw body with the sealed headers the plain API uses.
func writeWithHeaders(r *gin.Engine, board, body string, burn bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/boards/"+board, strings.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set(ghostboardModeHeader, "sealed")
	if burn {
		req.Header.Set(ghostboardBurnHeader, "true")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSealedWriteOnThePlainSurface(t *testing.T) {
	r, store := newSealedTestRouter(t)
	envelope := controllerEnvelope(t, "nota", "segreto")

	if w := writeWithHeaders(r, "nota", envelope, true); w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", w.Code, w.Body.String())
	}

	// A burn board is unreadable until it is opened, so its state is checked
	// through the open below; a sealed board without burn is read normally.
	vivo := controllerEnvelope(t, "viva", "segreto vivo")
	if w := writeWithHeaders(r, "viva", vivo, false); w.Code != http.StatusNoContent {
		t.Fatalf("seeding the sealed board: %d: %s", w.Code, w.Body.String())
	}
	board, err := store.Get("viva")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if board.Mode != ghostboard.ModeSealed || board.Burn || board.Text != vivo {
		t.Fatalf("unexpected board: %+v", board)
	}

	// A burn board cannot be read with a plain GET, on either surface.
	for _, path := range []string{"/api/boards/nota", "/ghostboard/nota"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusConflict {
			t.Fatalf("GET %s = %d, want 409: %s", path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), envelope) {
			t.Fatalf("GET %s revealed the envelope", path)
		}
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/boards/nota/open", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("open = %d, want 200: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != envelope {
		t.Fatalf("open returned %q, want the envelope", w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ghostboard/nota/open", nil))
	if w.Code != http.StatusGone {
		t.Fatalf("the second open = %d, want 410: %s", w.Code, w.Body.String())
	}
}

func TestSealedErrorEnvelopes(t *testing.T) {
	r, store := newSealedTestRouter(t)
	envelope := controllerEnvelope(t, "nota", "segreto")

	if w := writeWithHeaders(r, "nota", envelope, false); w.Code != http.StatusNoContent {
		t.Fatalf("seeding the sealed board: %d: %s", w.Code, w.Body.String())
	}

	cases := []struct {
		header map[string]string
		name   string
		method string
		path   string
		body   string
		error  string
		want   int
	}{
		{
			name:   "malformed envelope",
			method: http.MethodPost,
			path:   "/ghostboard/rotta",
			body:   `{"text":"nope","mode":"sealed"}`,
			header: map[string]string{"Content-Type": "application/json"},
			want:   http.StatusBadRequest,
			error:  "sealed envelope",
		},
		{
			name:   "plaintext write to a sealed board",
			method: http.MethodPost,
			path:   "/ghostboard/nota",
			body:   "plain text",
			header: map[string]string{"Content-Type": "text/plain"},
			want:   http.StatusConflict,
			error:  ghostboard.ErrPlaintextWriteToSealed.Error(),
		},
		{
			name:   "plaintext write through the plain surface",
			method: http.MethodPost,
			path:   "/api/boards/nota",
			body:   "plain text",
			header: map[string]string{"Content-Type": "text/plain"},
			want:   http.StatusConflict,
			error:  ghostboard.ErrPlaintextWriteToSealed.Error(),
		},
		{
			name:   "open a board that is not a burn board",
			method: http.MethodPost,
			path:   "/ghostboard/nota/open",
			want:   http.StatusConflict,
			error:  ghostboard.ErrBoardNotSealed.Error(),
		},
		{
			name:   "open an unknown board",
			method: http.MethodPost,
			path:   "/ghostboard/inesistente/open",
			want:   http.StatusNotFound,
			error:  ghostboard.ErrBoardNotFound.Error(),
		},
		{
			name:   "unknown mode",
			method: http.MethodPost,
			path:   "/ghostboard/nuova",
			body:   `{"text":"x","mode":"cifrata"}`,
			header: map[string]string{"Content-Type": "application/json"},
			want:   http.StatusBadRequest,
			error:  "use plain or sealed",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			req := httptest.NewRequest(testCase.method, testCase.path, strings.NewReader(testCase.body))
			for name, value := range testCase.header {
				req.Header.Set(name, value)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != testCase.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, testCase.want, w.Body.String())
			}

			var payload struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decoding the error envelope: %v", err)
			}
			if !strings.Contains(payload.Error, testCase.error) {
				t.Fatalf("error = %q, want it to mention %q", payload.Error, testCase.error)
			}
		})
	}

	// None of the failures above touched the sealed board.
	board, err := store.Get("nota")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if board.Text != envelope {
		t.Fatal("a refused request changed the envelope")
	}
}

func TestSealedDisabledThroughTheController(t *testing.T) {
	store, err := ghostboard.New(ghostboard.Config{
		ClearDelay:      time.Minute,
		SealedTombstone: time.Hour,
		MaxTextBytes:    4096,
		PersistInterval: time.Second,
		SealedEnabled:   false,
	})
	if err != nil {
		t.Fatalf("ghostboard.New() error = %v", err)
	}

	gc := NewGhostboardController(store, nil, 4096, time.Second)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/ghostboard/:board", gc.Update)

	envelope := controllerEnvelope(t, "nota", "segreto")
	w := writeWithHeaders(r, "nota", envelope, false)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
}
