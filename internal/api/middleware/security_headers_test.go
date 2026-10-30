package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// serveWithSecurityHeaders runs one GET through the middleware and returns the
// response.
func serveWithSecurityHeaders(t *testing.T, opts SecurityHeaderOptions) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeaders(opts))
	r.GET("/anything", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/anything", nil))
	return w
}

func TestSecurityHeadersAddsTheDefaultSet(t *testing.T) {
	w := serveWithSecurityHeaders(t, SecurityHeaderOptions{Enabled: true})

	if got := w.Header().Get("Content-Security-Policy"); got != DefaultContentSecurityPolicy {
		t.Errorf("Content-Security-Policy = %q, want the default policy", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want no-referrer", got)
	}
	if got := w.Header().Get("Permissions-Policy"); !strings.Contains(got, "camera=()") {
		t.Errorf("Permissions-Policy = %q, want the unused features turned off", got)
	}
	// HSTS off by default: the dashboard usually runs over plain HTTP on a
	// trusted network, where pinning HTTPS would lock the browser out.
	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security = %q, want none without hsts_max_age_secs", got)
	}
}

func TestSecurityHeadersUseTheConfiguredPolicy(t *testing.T) {
	policy := "default-src 'none'"
	w := serveWithSecurityHeaders(t, SecurityHeaderOptions{
		Enabled:               true,
		ContentSecurityPolicy: policy,
		HSTSMaxAge:            24 * time.Hour,
	})

	if got := w.Header().Get("Content-Security-Policy"); got != policy {
		t.Errorf("Content-Security-Policy = %q, want %q", got, policy)
	}
	if got := w.Header().Get("Strict-Transport-Security"); got != "max-age=86400" {
		t.Errorf("Strict-Transport-Security = %q, want max-age=86400", got)
	}
}

func TestSecurityHeadersDisabledAddsNothing(t *testing.T) {
	w := serveWithSecurityHeaders(t, SecurityHeaderOptions{
		Enabled:               false,
		ContentSecurityPolicy: "default-src 'none'",
		HSTSMaxAge:            time.Hour,
	})

	for _, name := range []string{
		"Content-Security-Policy",
		"X-Content-Type-Options",
		"Referrer-Policy",
		"Permissions-Policy",
		"Strict-Transport-Security",
	} {
		if got := w.Header().Get(name); got != "" {
			t.Errorf("%s = %q, want no header when the middleware is disabled", name, got)
		}
	}
}

// TestDefaultContentSecurityPolicyFitsTheUI pins the allowances the shipped
// pages need, so a later edit cannot quietly break Alpine, htmx, Tailwind or the
// dashboard icons.
func TestDefaultContentSecurityPolicyFitsTheUI(t *testing.T) {
	policy := DefaultContentSecurityPolicy

	required := []string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-eval'",
		"style-src 'self' 'unsafe-inline'",
		"https://cdn.jsdelivr.net",
		"https://unpkg.com",
		"https://cdn.tailwindcss.com",
		"img-src 'self' data: https: http:",
		"connect-src 'self'",
		"frame-ancestors 'none'",
	}
	for _, fragment := range required {
		if !strings.Contains(policy, fragment) {
			t.Errorf("the default policy does not carry %q", fragment)
		}
	}

	// Scripts must not carry 'unsafe-inline': the pages load every script from
	// a file, so an inline <script> injected into a page stays refused.
	script := directive(policy, "script-src")
	if strings.Contains(script, "'unsafe-inline'") {
		t.Errorf("script-src = %q, want no 'unsafe-inline'", script)
	}
}

// TestWaitingPageContentSecurityPolicyFitsTheBootPage pins what the policy of
// the waiting server has to allow: that page carries its redirect loop and its
// style inline, and loads nothing from another origin.
func TestWaitingPageContentSecurityPolicyFitsTheBootPage(t *testing.T) {
	policy := WaitingPageContentSecurityPolicy

	for _, fragment := range []string{
		"default-src 'none'",
		"script-src 'unsafe-inline'",
		"style-src 'unsafe-inline'",
		"connect-src 'self'",
		"frame-ancestors 'none'",
	} {
		if !strings.Contains(policy, fragment) {
			t.Errorf("the waiting-page policy does not carry %q", fragment)
		}
	}

	// The page evaluates nothing: the Function constructor stays closed.
	if strings.Contains(policy, "'unsafe-eval'") {
		t.Errorf("the waiting-page policy allows 'unsafe-eval': %q", policy)
	}
}

// TestSealedPageContentSecurityPolicyLoadsNothingElse pins the point of the
// sealed page: it holds the decryption key in location.hash, so a script from
// another origin could read the key and the plaintext. Every remote source stays
// closed, and scripts stay same-origin.
func TestSealedPageContentSecurityPolicyLoadsNothingElse(t *testing.T) {
	policy := SealedPageContentSecurityPolicy

	for _, fragment := range []string{
		"default-src 'none'",
		"script-src 'self' 'unsafe-eval'",
		"style-src 'self' 'unsafe-inline'",
		"connect-src 'self'",
		"object-src 'none'",
		"frame-ancestors 'none'",
	} {
		if !strings.Contains(policy, fragment) {
			t.Errorf("the sealed-page policy does not carry %q", fragment)
		}
	}

	for _, remote := range []string{"https://", "http://", "cdn.jsdelivr.net", "unpkg.com"} {
		if strings.Contains(policy, remote) {
			t.Errorf("the sealed-page policy allows the remote source %q: %s", remote, policy)
		}
	}

	if script := directive(policy, "script-src"); strings.Contains(script, "'unsafe-inline'") {
		t.Errorf("script-src = %q, want no 'unsafe-inline'", script)
	}
}

// directive returns one policy directive, for the assertions that care about
// what a single directive allows.
func directive(policy, name string) string {
	for _, part := range strings.Split(policy, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, name+" ") {
			return part
		}
	}
	return ""
}
