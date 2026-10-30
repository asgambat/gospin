package route

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/asgambat/gospin/internal/api/middleware"
	"github.com/asgambat/gospin/internal/config"
	"github.com/asgambat/gospin/internal/ghostboard"
)

func TestSetupRoutesAppliesTheSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// The UI pages are read from ./ui, relative to the project root.
	t.Chdir("../../..")

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	appCtx := newFileRouteApp(t, true, false)
	appCtx.Config.Server.SecurityHeaders = config.SecurityHeadersConfig{
		Enabled:    true,
		HSTSMaxAge: time.Hour,
	}
	r := SetupRoutes(appCtx, logger)

	// The API, the static UI and the health probe all carry the set.
	for _, path := range []string{"/health", "/ui/ghostboard", "/ui/assets/ghostboard.js"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, w.Code)
		}
		if got := w.Header().Get("Content-Security-Policy"); got != middleware.DefaultContentSecurityPolicy {
			t.Errorf("GET %s Content-Security-Policy = %q, want the default policy", path, got)
		}
		if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("GET %s X-Content-Type-Options = %q, want nosniff", path, got)
		}
		if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("GET %s Referrer-Policy = %q, want no-referrer", path, got)
		}
		if got := w.Header().Get("Strict-Transport-Security"); got != "max-age=3600" {
			t.Errorf("GET %s Strict-Transport-Security = %q, want max-age=3600", path, got)
		}
	}

	// An attachment is served with its own sandbox policy, which has to win
	// over the page policy: it must never run on the dashboard origin.
	w := routeUpload(t, r, "/ghostboard/team/files", "", "a.txt", "content")
	if w.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201: %s", w.Code, w.Body.String())
	}
	var stored ghostboard.File
	if err := json.Unmarshal(w.Body.Bytes(), &stored); err != nil {
		t.Fatalf("decoding the upload answer: %v", err)
	}

	download := httptest.NewRecorder()
	r.ServeHTTP(download, httptest.NewRequest(http.MethodGet, "/ghostboard/team/files/"+stored.ID, nil))
	if got := download.Header().Get("Content-Security-Policy"); got != "sandbox" {
		t.Errorf("download Content-Security-Policy = %q, want sandbox", got)
	}
}

func TestSetupRoutesLeavesTheHeadersOutWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	appCtx := newFileRouteApp(t, true, false)
	appCtx.Config.Server.SecurityHeaders = config.SecurityHeadersConfig{Enabled: false}
	r := SetupRoutes(appCtx, logger)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /health = %d, want 200", w.Code)
	}
	for _, name := range []string{
		"Content-Security-Policy",
		"X-Content-Type-Options",
		"Referrer-Policy",
		"Permissions-Policy",
		"Strict-Transport-Security",
	} {
		if got := w.Header().Get(name); got != "" {
			t.Errorf("%s = %q, want no header with security_headers.enabled false", name, got)
		}
	}
}
