package route

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/asgambat/gospin/internal/api/middleware"
	"github.com/asgambat/gospin/internal/config"
	"github.com/asgambat/gospin/internal/ghostboard"
	"github.com/asgambat/gospin/internal/sealedcrypto"
)

// testSealedRouter builds the routes with sealed boards switched on or off, the
// way ghostboard.sealed_enabled does in production.
func testSealedRouter(t *testing.T, enabled bool) (*gin.Engine, *ghostboard.Store) {
	t.Helper()

	cfg := config.DefaultGhostboardConfig()
	cfg.SealedEnabled = enabled
	cfg.MaxTextBytes = 4096

	store, err := ghostboard.New(ghostboard.Config{
		ClearDelay:      time.Minute,
		SealedTombstone: time.Hour,
		MaxTextBytes:    cfg.MaxTextBytes,
		PersistInterval: time.Second,
		SealedEnabled:   enabled,
		// Attachments are on, so the sealed refusal of a sealed board is
		// exercised instead of the "files disabled" answer.
		Files: ghostboard.FileConfig{
			Enabled:         true,
			Root:            t.TempDir(),
			MaxFileBytes:    1024,
			MaxBoardBytes:   4096,
			MaxTotalBytes:   8192,
			MaxFileCount:    3,
			TTL:             time.Hour,
			TransferTimeout: time.Minute,
		},
	})
	if err != nil {
		t.Fatalf("ghostboard.New() error = %v", err)
	}

	appCtx := testApp()
	appCtx.Config.Ghostboard = cfg
	appCtx.Ghostboard = store

	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	return SetupRoutes(appCtx, logger), store
}

// routeEnvelope seals a text for a board, returning the envelope the API stores.
func routeEnvelope(t *testing.T, board, text string) string {
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

// postSealed writes an envelope through the JSON API.
func postSealed(t *testing.T, r *gin.Engine, board, envelope string, burn bool) *httptest.ResponseRecorder {
	t.Helper()

	payload := map[string]any{"text": envelope, "mode": "sealed"}
	if burn {
		payload["burn"] = true
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshalling the body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/ghostboard/"+board, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSealedBoardRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r, _ := testSealedRouter(t, true)
	envelope := routeEnvelope(t, "nota", "segreto")

	t.Run("a sealed write creates the board", func(t *testing.T) {
		w := postSealed(t, r, "nota", envelope, false)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
		}

		var board ghostboard.Board
		if err := json.Unmarshal(w.Body.Bytes(), &board); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		if board.Mode != ghostboard.ModeSealed || board.Text != envelope {
			t.Fatalf("unexpected board: %+v", board)
		}
	})

	t.Run("a plain read serves the envelope uncached", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ghostboard/nota", nil))

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
		}
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", got)
		}
		var board ghostboard.Board
		if err := json.Unmarshal(w.Body.Bytes(), &board); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		if board.Text != envelope {
			t.Fatalf("the JSON API carries %q, want the envelope", board.Text)
		}
	})

	t.Run("a plaintext write to a sealed board is a conflict", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/ghostboard/nota", strings.NewReader("plain text"))
		req.Header.Set("Content-Type", "text/plain")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
		}
	})

	t.Run("a malformed envelope is a bad request", func(t *testing.T) {
		if w := postSealed(t, r, "rotta", "not an envelope", false); w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
		}
	})

	t.Run("an unknown mode is a bad request", func(t *testing.T) {
		body := `{"text":"` + envelope + `","mode":"cifrato"}`
		req := httptest.NewRequest(http.MethodPost, "/ghostboard/inventata", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
		}
	})

	t.Run("opening a board that is not a burn board is a conflict", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ghostboard/nota/open", nil))

		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
		}
	})

	t.Run("attachments on a sealed board are a conflict", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ghostboard/nota/files", nil))

		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
		}
	})
}

func TestBurnBoardRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r, _ := testSealedRouter(t, true)
	envelope := routeEnvelope(t, "effimera", "una volta sola")

	if w := postSealed(t, r, "effimera", envelope, true); w.Code != http.StatusOK {
		t.Fatalf("creating the burn board: status = %d: %s", w.Code, w.Body.String())
	}

	t.Run("a plain read reveals nothing", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ghostboard/effimera", nil))

		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), envelope) {
			t.Fatal("the refused read still carried the envelope")
		}
	})

	t.Run("the event stream reveals nothing", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ghostboard/effimera/events", nil))

		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409: %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), envelope) {
			t.Fatal("the refused stream still carried the envelope")
		}
	})

	t.Run("the plain open endpoint returns the envelope", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/boards/effimera/open", nil))

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
		}
		if w.Body.String() != envelope {
			t.Fatalf("the open endpoint returned %q, want the envelope", w.Body.String())
		}
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", got)
		}
	})

	t.Run("the second open is gone", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ghostboard/effimera/open", nil))

		if w.Code != http.StatusGone {
			t.Fatalf("status = %d, want 410: %s", w.Code, w.Body.String())
		}
	})

	t.Run("a later read is gone too", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ghostboard/effimera", nil))

		if w.Code != http.StatusGone {
			t.Fatalf("status = %d, want 410: %s", w.Code, w.Body.String())
		}
	})
}

func TestSealedDisabledHidesEverything(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r, _ := testSealedRouter(t, false)
	envelope := routeEnvelope(t, "nota", "segreto")

	t.Run("a sealed write is not found", func(t *testing.T) {
		if w := postSealed(t, r, "nota", envelope, false); w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
		}
	})

	t.Run("the open route is not registered", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/ghostboard/nota/open", nil))

		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
		}
	})

	t.Run("the sealed page is not served", func(t *testing.T) {
		t.Chdir("../../..")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ui/sealed/nota", nil))

		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
		}
	})

	t.Run("a plain board keeps working", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/ghostboard/nota", strings.NewReader("plain text"))
		req.Header.Set("Content-Type", "text/plain")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
		}
	})
}

func TestSealedPageCarriesItsOwnPolicy(t *testing.T) {
	t.Chdir("../../..")
	gin.SetMode(gin.TestMode)
	r, _ := testSealedRouter(t, true)

	for _, path := range []string{"/ui/sealed", "/ui/sealed/nota"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
			}
			if got := w.Header().Get("Content-Security-Policy"); got != middleware.SealedPageContentSecurityPolicy {
				t.Fatalf("Content-Security-Policy = %q, want the sealed policy", got)
			}
			if got := w.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}

			body := w.Body.String()
			if !strings.Contains(body, `data-max-plaintext-bytes="`) {
				t.Error("the page does not carry the plaintext limit")
			}
			if strings.Contains(body, `data-max-plaintext-bytes=""`) {
				t.Error("the plaintext limit was not injected")
			}
			if !strings.Contains(body, "/ui/assets/sealed.js") {
				t.Error("the page does not load its own script")
			}
			// The page holds the key: it must not register the shared service
			// worker, whose cache could replay an envelope.
			if strings.Contains(body, "sw-register.js") {
				t.Error("the sealed page registers the service worker")
			}
			// Nor may it load anything from another origin.
			if strings.Contains(body, "https://") {
				t.Errorf("the sealed page references a remote origin: %s", body)
			}
		})
	}
}

func TestSealedRouteNeedsThePINOfAPrivateBoard(t *testing.T) {
	t.Chdir("../../..")
	gin.SetMode(gin.TestMode)

	digest, err := ghostboard.PINDigest(routePINSecret, "1234")
	if err != nil {
		t.Fatalf("PINDigest() error = %v", err)
	}
	auth, err := ghostboard.NewAuthenticator(ghostboard.AuthConfig{
		PINDigest:     digest,
		Secret:        routePINSecret,
		PrivateBoards: []string{"lavoro"},
		SessionTTL:    time.Hour,
		MaxAttempts:   3,
		Lockout:       time.Minute,
	})
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}

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

	cfg := config.DefaultGhostboardConfig()
	cfg.MaxTextBytes = 4096

	appCtx := testApp()
	appCtx.Config.Ghostboard = cfg
	appCtx.Ghostboard = store
	appCtx.GhostboardAuth = auth

	logger := logrus.New()
	router := SetupRoutes(appCtx, logger)

	open := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/ghostboard/lavoro/open", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	// Seed a burn board directly: the store refuses a sealed write on a private
	// board without a session, and the point here is the route gate.
	key, err := sealedcrypto.NewKey()
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	envelope, err := sealedcrypto.Seal(key, "lavoro", []byte("riservata"))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if _, err := store.UpdateSealed(ghostboard.SealedWrite{Name: "lavoro", Envelope: envelope, Burn: true}); err != nil {
		t.Fatalf("UpdateSealed() error = %v", err)
	}

	if w := open(""); w.Code != http.StatusUnauthorized {
		t.Fatalf("an unauthenticated open returned %d, want 401: %s", w.Code, w.Body.String())
	}

	unlock := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ghostboard/lavoro/unlock", strings.NewReader(`{"pin":"1234"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(unlock, req)
	if unlock.Code != http.StatusOK {
		t.Fatalf("unlock returned %d: %s", unlock.Code, unlock.Body.String())
	}

	var token struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(unlock.Body.Bytes(), &token); err != nil {
		t.Fatalf("decoding the unlock response: %v", err)
	}

	w := open(token.Token)
	if w.Code != http.StatusOK {
		t.Fatalf("an authenticated open returned %d, want 200: %s", w.Code, w.Body.String())
	}

	var opened ghostboard.Board
	if err := json.Unmarshal(w.Body.Bytes(), &opened); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if opened.Text != envelope {
		t.Fatal("the authenticated open did not return the envelope")
	}
}

// TestSealedEntryPointOnTheOtherPages pins where creation is reached from:
// the ghostboard page and the home page carry a navigation-only link to
// /ui/sealed while the feature is enabled, and neither a dead link nor the
// marker comment once it is disabled. The link never hands the key over:
// the CDN-policy pages do not handle it (ADR-0010), /ui/sealed does.
func TestSealedEntryPointOnTheOtherPages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Chdir("../../..")

	for _, enabled := range []bool{true, false} {
		router := gin.New()
		NewUIRouter(router, GhostboardPageConfig{MaxTextBytes: 4096, SealedEnabled: enabled})

		for _, path := range []string{"/ui/ghostboard", "/ui/home"} {
			t.Run(fmt.Sprintf("%s sealed_enabled=%t", path, enabled), func(t *testing.T) {
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))

				if w.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
				}
				body := w.Body.String()

				if enabled {
					if !strings.Contains(body, `href="/ui/sealed"`) {
						t.Error("the page carries no link to /ui/sealed")
					}
				} else {
					if strings.Contains(body, `href="/ui/sealed"`) {
						t.Error("the page links to a feature that is disabled")
					}
				}

				// A leftover marker comment in the response means the handler
				// forgot the injection entirely; the page source itself is not
				// part of this assertion, only what the server serves.
				if strings.Contains(body, "SEALED_BOARD_ENTRY") {
					t.Error("the marker comment reached the served page")
				}
			})
		}
	}
}
