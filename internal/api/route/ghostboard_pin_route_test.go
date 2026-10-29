package route

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/asgambat/gospin/internal/app"
	"github.com/asgambat/gospin/internal/config"
	"github.com/asgambat/gospin/internal/ghostboard"
)

const routePINSecret = "0123456789abcdef-secret"

// testPINApp is testApp with a PIN protecting the "lavoro" board.
func testPINApp(t *testing.T) *app.App {
	t.Helper()

	digest, err := ghostboard.PINDigest(routePINSecret, "1234")
	if err != nil {
		t.Fatalf("PINDigest: %v", err)
	}

	appCtx := testApp()
	appCtx.Config.Ghostboard = config.GhostboardConfig{
		ClearDelay:        time.Minute,
		MaxTextBytes:      1024,
		PersistInterval:   time.Second,
		HeartbeatInterval: 20 * time.Millisecond,
		PINHMAC:           digest,
		PINSecret:         routePINSecret,
		PrivateBoards:     []string{"lavoro"},
		SessionTTL:        time.Hour,
		PINMaxAttempts:    3,
		PINLockout:        time.Minute,
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
		t.Fatalf("NewAuthenticator: %v", err)
	}
	appCtx.GhostboardAuth = auth

	return appCtx
}

func TestGhostboardPINRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	appCtx := testPINApp(t)
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	router := SetupRoutes(appCtx, logger)

	privateRequests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "json read", method: http.MethodGet, path: "/ghostboard/lavoro"},
		{name: "json write", method: http.MethodPost, path: "/ghostboard/lavoro"},
		{name: "plain text read", method: http.MethodGet, path: "/api/boards/lavoro"},
		{name: "plain text write", method: http.MethodPost, path: "/api/boards/lavoro"},
		{name: "event stream", method: http.MethodGet, path: "/ghostboard/lavoro/events"},
	}

	for _, request := range privateRequests {
		t.Run(request.name+" needs the pin", func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(request.method, request.path, strings.NewReader("secret text")))

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
			}

			var body struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding the error body: %v", err)
			}
			if body.Error != "pin required" {
				t.Errorf("error = %q, want \"pin required\"", body.Error)
			}
		})
	}

	t.Run("a public board stays open", func(t *testing.T) {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ghostboard/note", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("unlock then read and write", func(t *testing.T) {
		unlock := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/ghostboard/lavoro/unlock", strings.NewReader(`{"pin":"1234"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(unlock, req)

		if unlock.Code != http.StatusOK {
			t.Fatalf("unlock status = %d, want 200 (body %s)", unlock.Code, unlock.Body.String())
		}
		cookies := unlock.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("unlock returned no session cookie")
		}

		write := httptest.NewRecorder()
		writeReq := httptest.NewRequest(http.MethodPost, "/ghostboard/lavoro", strings.NewReader(`{"text":"riservato"}`))
		writeReq.Header.Set("Content-Type", "application/json")
		writeReq.AddCookie(cookies[0])
		router.ServeHTTP(write, writeReq)
		if write.Code != http.StatusOK {
			t.Fatalf("write status = %d, want 200 (body %s)", write.Code, write.Body.String())
		}

		read := httptest.NewRecorder()
		readReq := httptest.NewRequest(http.MethodGet, "/ghostboard/lavoro", nil)
		readReq.AddCookie(cookies[0])
		router.ServeHTTP(read, readReq)

		var board ghostboard.Board
		if err := json.Unmarshal(read.Body.Bytes(), &board); err != nil {
			t.Fatalf("decoding the board: %v", err)
		}
		if board.Text != "riservato" {
			t.Errorf("Text = %q, want \"riservato\"", board.Text)
		}
	})

	t.Run("the bearer token works for scripts", func(t *testing.T) {
		unlock := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/ghostboard/lavoro/unlock", strings.NewReader(`{"pin":"1234"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(unlock, req)

		var body struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(unlock.Body.Bytes(), &body); err != nil {
			t.Fatalf("decoding the unlock response: %v", err)
		}

		read := httptest.NewRecorder()
		readReq := httptest.NewRequest(http.MethodGet, "/api/boards/lavoro", nil)
		readReq.Header.Set("Authorization", "Bearer "+body.Token)
		router.ServeHTTP(read, readReq)

		if read.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", read.Code, read.Body.String())
		}
		if read.Body.String() != "riservato" {
			t.Errorf("text = %q, want \"riservato\"", read.Body.String())
		}
	})

	t.Run("lock ends the session of the browser", func(t *testing.T) {
		unlock := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/ghostboard/lavoro/unlock", strings.NewReader(`{"pin":"1234"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(unlock, req)
		cookies := unlock.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatal("unlock returned no session cookie")
		}

		lock := httptest.NewRecorder()
		lockReq := httptest.NewRequest(http.MethodDelete, "/ghostboard/lavoro/unlock", nil)
		lockReq.AddCookie(cookies[0])
		router.ServeHTTP(lock, lockReq)
		if lock.Code != http.StatusNoContent {
			t.Fatalf("lock status = %d, want 204", lock.Code)
		}

		expired := lock.Result().Cookies()
		if len(expired) != 1 || expired[0].MaxAge >= 0 {
			t.Fatalf("lock did not expire the cookie: %v", expired)
		}

		read := httptest.NewRecorder()
		readReq := httptest.NewRequest(http.MethodGet, "/ghostboard/lavoro", nil)
		readReq.AddCookie(expired[0])
		router.ServeHTTP(read, readReq)
		if read.Code != http.StatusUnauthorized {
			t.Errorf("status after lock = %d, want 401", read.Code)
		}
	})
}
