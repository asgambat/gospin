package route

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestUIRoute_ServiceWorkerServedWithCorrectHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// go test runs from the package directory (internal/api/route/),
	// but c.File() paths are relative to the project root.
	// t.Chdir is scoped to this test — no global CWD mutation.
	t.Chdir("../../..")

	r := gin.New()
	NewUIRouter(r)

	req, _ := http.NewRequest(http.MethodGet, "/ui/service-worker.js", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// Status
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Content-Type
	contentType := w.Header().Get("Content-Type")
	if contentType != "application/javascript; charset=utf-8" {
		t.Errorf("expected Content-Type 'application/javascript; charset=utf-8', got '%s'", contentType)
	}

	// Service-Worker-Allowed
	swAllowed := w.Header().Get("Service-Worker-Allowed")
	if swAllowed != "/" {
		t.Errorf("expected Service-Worker-Allowed '/', got '%s'", swAllowed)
	}

	// Cache-Control
	cacheControl := w.Header().Get("Cache-Control")
	if !strings.Contains(cacheControl, "max-age=3600") {
		t.Errorf("expected Cache-Control to contain 'max-age=3600', got '%s'", cacheControl)
	}

	// Body is non-empty JavaScript
	body := w.Body.String()
	if len(body) == 0 {
		t.Error("expected non-empty response body")
	}
	if !strings.Contains(body, "CACHE_NAME") {
		t.Error("expected response body to contain service worker code")
	}
}

func TestUIRoute_CacheBusterInjected(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Chdir("../../..")

	r := gin.New()
	NewUIRouter(r)

	cases := []struct {
		name   string
		path   string
		needle string
		buster string
	}{
		{"index", "/ui", `src="/ui/assets/app.js?v=`, appJSCacheBuster},
		{"home", "/ui/home", `src="/ui/home.js?v=`, homeJSCacheBuster},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", w.Code)
			}

			body := w.Body.String()
			if !strings.Contains(body, tc.needle+tc.buster) {
				t.Errorf("expected body to contain %q, got a snippet: %s", tc.needle+tc.buster, body[:min(len(body), 200)])
			}
			// The original un-busted tag must no longer be present.
			if strings.Contains(body, tc.needle[:len(tc.needle)-3]+`"`) {
				t.Errorf("expected original un-busted tag to be rewritten, body snippet: %s", body[:min(len(body), 200)])
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
