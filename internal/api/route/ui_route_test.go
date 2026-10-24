package route

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	NewUIRouter(r, 16777216)

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

func TestUIRoute_ServiceWorkerRegistrationServedWithCorrectHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Chdir("../../..")

	r := gin.New()
	NewUIRouter(r, 16777216)

	req, _ := http.NewRequest(http.MethodGet, "/ui/sw-register.js", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/javascript; charset=utf-8" {
		t.Errorf("expected Content-Type 'application/javascript; charset=utf-8', got '%s'", ct)
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "max-age=3600") {
		t.Errorf("expected Cache-Control to contain 'max-age=3600', got '%s'", cc)
	}

	body := w.Body.String()
	for _, needle := range []string{"/ui/service-worker.js", "scope: '/'"} {
		if !strings.Contains(body, needle) {
			t.Errorf("expected the registration script to contain %q", needle)
		}
	}
}

// TestUIRoute_PagesShareServiceWorkerRegistration pins the single registration
// point: every page loads the shared script, and none registers inline, so a
// device that opens only one page still gets the service worker.
func TestUIRoute_PagesShareServiceWorkerRegistration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Chdir("../../..")

	r := gin.New()
	NewUIRouter(r, 16777216)

	for _, path := range []string{"/ui", "/ui/home", "/ui/ghostboard"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))

			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", w.Code)
			}

			body := w.Body.String()
			if !strings.Contains(body, `src="/ui/sw-register.js"`) {
				t.Error("page does not load the shared service worker registration script")
			}
			if strings.Contains(body, "navigator.serviceWorker.register") {
				t.Error("page still registers the service worker inline instead of using /ui/sw-register.js")
			}
		})
	}
}

func TestUIRoute_CacheBusterInjected(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Chdir("../../..")

	r := gin.New()
	NewUIRouter(r, 16777216)

	cases := []struct {
		name   string
		path   string
		needle string
		bundle string
	}{
		{"index", "/ui", `src="/ui/assets/app.js?v=`, "assets/app.js"},
		{"home", "/ui/home", `src="/ui/home.js?v=`, "home.js"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A "dev" hash means the bundle could not be read: the cache-busting
			// would silently stop working, so require a real content hash.
			buster := assetVersion(tc.bundle)
			if buster == "dev" || len(buster) != 8 {
				t.Fatalf("cache-buster = %q, want 8 hex characters", buster)
			}

			req, _ := http.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", w.Code)
			}

			body := w.Body.String()
			if !strings.Contains(body, tc.needle+buster) {
				t.Errorf("expected body to contain %q, got a snippet: %s", tc.needle+buster, body[:min(len(body), 200)])
			}
			// The original un-busted tag must no longer be present.
			if strings.Contains(body, tc.needle[:len(tc.needle)-3]+`"`) {
				t.Errorf("expected original un-busted tag to be rewritten, body snippet: %s", body[:min(len(body), 200)])
			}
		})
	}
}

func TestUIRoute_FirstExistingDirPrefersFirstMatch(t *testing.T) {
	tmp := t.TempDir()
	first := filepath.Join(tmp, "first")
	second := filepath.Join(tmp, "second")
	file := filepath.Join(tmp, "file.txt")

	for _, dir := range []string{first, second} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", file, err)
	}

	if got := firstExistingDir(file, first, second); got != first {
		t.Errorf("firstExistingDir() = %q, want the first existing directory %q", got, first)
	}
	if got := firstExistingDir(file, filepath.Join(tmp, "missing")); got != "" {
		t.Errorf("firstExistingDir() = %q, want empty when no candidate is a directory", got)
	}
}
