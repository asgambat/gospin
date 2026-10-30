package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/asgambat/gospin/internal/ghostboard"
)

const testBoardSecret = "0123456789abcdef-secret"

func newTestPINAuth(t *testing.T) *ghostboard.Authenticator {
	t.Helper()

	digest, err := ghostboard.PINDigest(testBoardSecret, "1234")
	if err != nil {
		t.Fatalf("PINDigest: %v", err)
	}
	auth, err := ghostboard.NewAuthenticator(ghostboard.AuthConfig{
		PINDigest:     digest,
		Secret:        testBoardSecret,
		PrivateBoards: []string{"lavoro"},
		SessionTTL:    time.Hour,
		MaxAttempts:   3,
		Lockout:       time.Minute,
	})
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	return auth
}

func newPINRouter(auth *ghostboard.Authenticator) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/ghostboard/:board", GhostboardPIN(auth), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return router
}

func TestGhostboardPIN(t *testing.T) {
	auth := newTestPINAuth(t)
	router := newPINRouter(auth)
	token, _ := auth.IssueToken()

	tests := []struct {
		name   string
		board  string
		cookie string
		bearer string
		status int
	}{
		{name: "private board without session", board: "lavoro", status: http.StatusUnauthorized},
		{name: "private board with cookie", board: "lavoro", cookie: token, status: http.StatusOK},
		{name: "private board with bearer", board: "lavoro", bearer: token, status: http.StatusOK},
		{name: "private board with a junk token", board: "lavoro", cookie: "nope", status: http.StatusUnauthorized},
		{name: "private board with another scheme", board: "lavoro", bearer: "Basic abc", status: http.StatusUnauthorized},
		{name: "public board", board: "note", status: http.StatusOK},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/ghostboard/"+test.board, nil)
			if test.cookie != "" {
				req.AddCookie(&http.Cookie{Name: ghostboard.SessionCookie, Value: test.cookie})
			}
			if test.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+test.bearer)
			}

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != test.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, test.status, rec.Body.String())
			}
		})
	}
}

func TestGhostboardPIN_ErrorBody(t *testing.T) {
	auth := newTestPINAuth(t)
	router := newPINRouter(auth)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ghostboard/lavoro", nil))

	var body struct {
		Error string `json:"error"`
		Board string `json:"board"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding the error body: %v", err)
	}
	if body.Error != ghostboard.ErrPINRequired.Error() {
		t.Errorf("error = %q, want %q", body.Error, ghostboard.ErrPINRequired.Error())
	}
	if body.Board != "lavoro" {
		t.Errorf("board = %q, want lavoro", body.Board)
	}
}

func TestGhostboardPIN_WithoutConfiguredPIN(t *testing.T) {
	router := newPINRouter(nil)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ghostboard/lavoro", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: without a PIN every board is public", rec.Code)
	}
}
