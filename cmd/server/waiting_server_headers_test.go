package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/asgambat/gospin/internal/api/middleware"
	"github.com/asgambat/gospin/internal/config"
)

// TestWaitingServerSendsSecurityHeaders pins the headers of the second server:
// the boot page is a browser-facing HTML page on its own port, so leaving it
// out would keep a surface without a policy.
func TestWaitingServerSendsSecurityHeaders(t *testing.T) {
	appCtx := newTestAppCtx(newMockRuntime(), newTestStore())
	appCtx.Config.Server.SecurityHeaders = config.SecurityHeadersConfig{
		Enabled:    true,
		HSTSMaxAge: 24 * time.Hour,
	}

	r := newWaitingRouter(appCtx, logrus.New())

	for _, path := range []string{"/Deluge", "/container/Deluge/ready"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))

		if got := w.Header().Get("Content-Security-Policy"); got != middleware.WaitingPageContentSecurityPolicy {
			t.Errorf("%s: Content-Security-Policy = %q, want the waiting-page policy", path, got)
		}
		if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q, want nosniff", path, got)
		}
		if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("%s: Referrer-Policy = %q, want no-referrer", path, got)
		}
		if got := w.Header().Get("Permissions-Policy"); got == "" {
			t.Errorf("%s: Permissions-Policy is empty, want the unused features turned off", path)
		}
		if got := w.Header().Get("Strict-Transport-Security"); got != "max-age=86400" {
			t.Errorf("%s: Strict-Transport-Security = %q, want max-age=86400", path, got)
		}
	}
}

// TestWaitingServerHeadersFollowTheSwitch keeps `enabled: false` meaning no
// header at all, on both servers.
func TestWaitingServerHeadersFollowTheSwitch(t *testing.T) {
	appCtx := newTestAppCtx(newMockRuntime(), newTestStore())
	appCtx.Config.Server.SecurityHeaders = config.SecurityHeadersConfig{
		Enabled:    false,
		HSTSMaxAge: time.Hour,
	}

	r := newWaitingRouter(appCtx, logrus.New())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/Deluge", nil))

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
