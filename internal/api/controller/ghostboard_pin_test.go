package controller

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/asgambat/gospin/internal/ghostboard"
)

const pinTestSecret = "0123456789abcdef-secret"

// newPINTestRouter builds a controller whose "lavoro" board is private, with
// the unlock and lock routes wired like the real router does.
func newPINTestRouter(t *testing.T) *gin.Engine {
	t.Helper()

	digest, err := ghostboard.PINDigest(pinTestSecret, "1234")
	if err != nil {
		t.Fatalf("PINDigest: %v", err)
	}
	auth, err := ghostboard.NewAuthenticator(ghostboard.AuthConfig{
		PINDigest:     digest,
		Secret:        pinTestSecret,
		PrivateBoards: []string{"lavoro"},
		SessionTTL:    time.Hour,
		MaxAttempts:   2,
		Lockout:       time.Minute,
	})
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}

	store, err := ghostboard.New(ghostboard.Config{
		ClearDelay:      time.Minute,
		MaxTextBytes:    1024,
		PersistInterval: time.Second,
	})
	if err != nil {
		t.Fatalf("ghostboard.New() error = %v", err)
	}

	gc := NewGhostboardController(store, auth, 1024, time.Second)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/ghostboard/:board/unlock", gc.Unlock)
	router.DELETE("/ghostboard/:board/unlock", gc.Lock)
	return router
}

func postJSON(t *testing.T, router *gin.Engine, path string, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestGhostboardUnlock(t *testing.T) {
	router := newPINTestRouter(t)

	rec := postJSON(t, router, "/ghostboard/lavoro/unlock", `{"pin":"1234"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	var body struct {
		ExpiresAt time.Time `json:"expires_at"`
		Token     string    `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding the response: %v", err)
	}
	if body.Token == "" {
		t.Error("the response carries no token")
	}
	if time.Until(body.ExpiresAt) <= 0 {
		t.Errorf("expires_at = %v, want a future time", body.ExpiresAt)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %v, want one session cookie", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != ghostboard.SessionCookie {
		t.Errorf("cookie name = %q, want %q", cookie.Name, ghostboard.SessionCookie)
	}
	if cookie.Value != body.Token {
		t.Error("the cookie does not carry the issued token")
	}
	if !cookie.HttpOnly {
		t.Error("the session cookie must be HttpOnly")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSite = %v, want Strict", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Errorf("cookie path = %q, want /", cookie.Path)
	}
	if cookie.MaxAge != int(time.Hour.Seconds()) {
		t.Errorf("cookie MaxAge = %d, want %d", cookie.MaxAge, int(time.Hour.Seconds()))
	}
}

func TestGhostboardUnlockAcceptsAFormBody(t *testing.T) {
	router := newPINTestRouter(t)

	form := url.Values{"pin": {"1234"}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/ghostboard/lavoro/unlock", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestGhostboardUnlockRejectsWrongPIN(t *testing.T) {
	router := newPINTestRouter(t)

	rec := postJSON(t, router, "/ghostboard/lavoro/unlock", `{"pin":"9999"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if cookies := rec.Result().Cookies(); len(cookies) != 0 {
		t.Errorf("a failed unlock set a cookie: %v", cookies)
	}

	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding the error body: %v", err)
	}
	if body.Error != "invalid pin" {
		t.Errorf("error = %q, want \"invalid pin\"", body.Error)
	}
}

func TestGhostboardUnlockRejectsAMalformedPIN(t *testing.T) {
	router := newPINTestRouter(t)

	for _, body := range []string{`{"pin":"12"}`, `{"pin":""}`, `{}`} {
		rec := postJSON(t, router, "/ghostboard/lavoro/unlock", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", body, rec.Code)
		}
	}
}

func TestGhostboardUnlockLocksOutAfterFailures(t *testing.T) {
	router := newPINTestRouter(t)

	for i := 0; i < 2; i++ {
		if rec := postJSON(t, router, "/ghostboard/lavoro/unlock", `{"pin":"9999"}`); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i+1, rec.Code)
		}
	}

	rec := postJSON(t, router, "/ghostboard/lavoro/unlock", `{"pin":"1234"}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "60" {
		t.Errorf("Retry-After = %q, want 60", rec.Header().Get("Retry-After"))
	}

	var body struct {
		Error     string `json:"error"`
		RetrySecs int    `json:"retry_after_secs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding the error body: %v", err)
	}
	if body.Error != "too many pin attempts" || body.RetrySecs != 60 {
		t.Errorf("body = %+v, want the lockout envelope", body)
	}
}

func TestGhostboardLockExpiresTheCookie(t *testing.T) {
	router := newPINTestRouter(t)

	req := httptest.NewRequest(http.MethodDelete, "/ghostboard/lavoro/unlock", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != ghostboard.SessionCookie {
		t.Fatalf("cookies = %v, want one expiring session cookie", cookies)
	}
	if cookies[0].MaxAge >= 0 {
		t.Errorf("cookie MaxAge = %d, want a negative value (expired)", cookies[0].MaxAge)
	}
}

// TestGhostboardUnlockOnPublicBoard covers the scope of the endpoint: a board
// that is not in ghostboard.private_boards has no PIN to trade, so it must not
// hand out a session for the boards that do.
func TestGhostboardUnlockOnPublicBoard(t *testing.T) {
	router := newPINTestRouter(t)

	for _, board := range []string{"note", "default", "___"} {
		rec := postJSON(t, router, "/ghostboard/"+board+"/unlock", `{"pin":"1234"}`)
		if rec.Code != http.StatusNotFound {
			t.Errorf("board %q: status = %d, want 404", board, rec.Code)
		}
		if cookies := rec.Result().Cookies(); len(cookies) != 0 {
			t.Errorf("board %q: a refused unlock set a cookie: %v", board, cookies)
		}
	}
}

// TestGhostboardEventsEndsWhenTheSessionExpires covers the stream of a private
// board: the session is checked when the stream opens, so without a re-check an
// open page would keep receiving the text of a private board long after its
// token expired.
func TestGhostboardEventsEndsWhenTheSessionExpires(t *testing.T) {
	digest, err := ghostboard.PINDigest(pinTestSecret, "1234")
	if err != nil {
		t.Fatalf("PINDigest: %v", err)
	}
	auth, err := ghostboard.NewAuthenticator(ghostboard.AuthConfig{
		PINDigest:     digest,
		Secret:        pinTestSecret,
		PrivateBoards: []string{"lavoro"},
		SessionTTL:    300 * time.Millisecond,
		MaxAttempts:   3,
		Lockout:       time.Minute,
	})
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}

	store, err := ghostboard.New(ghostboard.Config{
		ClearDelay:      time.Minute,
		MaxTextBytes:    1024,
		PersistInterval: time.Second,
	})
	if err != nil {
		t.Fatalf("ghostboard.New: %v", err)
	}

	gc := NewGhostboardController(store, auth, 1024, 10*time.Millisecond)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/ghostboard/:board/events", gc.Events)

	token, _ := auth.IssueToken()
	server := httptest.NewServer(router)
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/ghostboard/lavoro/events", nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("opening the stream: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// The write lands after the 300ms token expired: it must not reach the
	// stream, whose next event is the expiry notice instead.
	time.Sleep(400 * time.Millisecond)
	if _, err := store.Update("lavoro", "scritto-dopo-la-scadenza", ""); err != nil {
		t.Fatalf("store.Update: %v", err)
	}

	// Reading to the end returns only when the handler closes the stream, which
	// the expiry check does once the token is spent.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the stream: %v", err)
	}
	if !strings.Contains(string(body), "event: locked") {
		t.Fatalf("the stream did not announce the expired session: %q", body)
	}
	if strings.Contains(string(body), "scritto-dopo-la-scadenza") {
		t.Fatalf("the stream delivered a write after its session expired: %q", body)
	}
}

func TestGhostboardUnlockWithoutConfiguredPIN(t *testing.T) {
	gc := NewGhostboardController(nil, nil, 1024, time.Second)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/ghostboard/:board/unlock", gc.Unlock)

	rec := postJSON(t, router, "/ghostboard/lavoro/unlock", `{"pin":"1234"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when no PIN is configured", rec.Code)
	}
}
