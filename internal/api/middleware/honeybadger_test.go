package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func TestHoneybadgerMiddleware_NoAPIKey_PassThrough(t *testing.T) {
	orig := os.Getenv("HONEYBADGER_API_KEY")
	os.Unsetenv("HONEYBADGER_API_KEY")
	defer func() { os.Setenv("HONEYBADGER_API_KEY", orig) }()

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	r := gin.New()
	r.Use(HoneybadgerMiddleware(logger))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestHoneybadgerMiddleware_NoAPIKey_404PassesThrough(t *testing.T) {
	orig := os.Getenv("HONEYBADGER_API_KEY")
	os.Unsetenv("HONEYBADGER_API_KEY")
	defer func() { os.Setenv("HONEYBADGER_API_KEY", orig) }()

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	r := gin.New()
	r.Use(HoneybadgerMiddleware(logger))

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}
