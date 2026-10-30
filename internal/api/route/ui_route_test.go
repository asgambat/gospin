package route

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
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
	NewUIRouter(r, GhostboardPageConfig{MaxTextBytes: 16777216, MaxFileBytes: 838860800})

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

// TestUIRoute_GhostboardPageOffersTheFilesArchive pins the archive link of the
// Files panel to the route the server serves, so the page and the API cannot
// drift apart without a test noticing.
func TestUIRoute_GhostboardPageOffersTheFilesArchive(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Chdir("../../..")

	r := gin.New()
	NewUIRouter(r, GhostboardPageConfig{MaxTextBytes: 16777216, MaxFileBytes: 838860800})

	// The page binds the link to the helper and the helper builds the route,
	// so both halves are pinned.
	tests := []struct {
		path   string
		needle string
	}{
		{path: "/ui/ghostboard", needle: "archiveUrl()"},
		{path: "/ui/assets/ghostboard.js", needle: "'/files.zip'"},
	}

	for _, tt := range tests {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.path, nil))

		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", tt.path, w.Code)
		}
		if !strings.Contains(w.Body.String(), tt.needle) {
			t.Errorf("GET %s does not offer the archive of the board (missing %q)", tt.path, tt.needle)
		}
	}
}

func TestUIRoute_ServiceWorkerRegistrationServedWithCorrectHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Chdir("../../..")

	r := gin.New()
	NewUIRouter(r, GhostboardPageConfig{MaxTextBytes: 16777216, MaxFileBytes: 838860800})

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
	NewUIRouter(r, GhostboardPageConfig{MaxTextBytes: 16777216, MaxFileBytes: 838860800})

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
	NewUIRouter(r, GhostboardPageConfig{MaxTextBytes: 16777216, MaxFileBytes: 838860800})

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

// TestUIRoute_PagesLoadNoExternalAssets pins the offline requirement: the shell
// of every page has to come from this server. A CDN reference here means the
// dashboard and the ghostboard paint an unstyled page with no JavaScript on a
// LAN that has no internet access.
func TestUIRoute_PagesLoadNoExternalAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Chdir("../../..")

	r := gin.New()
	NewUIRouter(r, GhostboardPageConfig{MaxTextBytes: 16777216, MaxFileBytes: 838860800})

	cases := []struct {
		name   string
		path   string
		vendor []string
	}{
		{name: "index", path: "/ui", vendor: []string{vendorAlpineJS, vendorHtmx, vendorTailwind}},
		{name: "home", path: "/ui/home", vendor: []string{vendorAlpineCollapse, vendorAlpineJS, vendorHtmx, vendorTailwind}},
		{name: "ghostboard", path: "/ui/ghostboard", vendor: []string{vendorAlpineJS, vendorTailwind}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))

			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", w.Code)
			}

			body := w.Body.String()
			for _, external := range []string{`src="http`, `href="http`} {
				if strings.Contains(body, external) {
					t.Errorf("the page loads an external asset (%s), so it breaks without internet", external)
				}
			}

			// Every library must come from the server and carry the content
			// hash, so an upgraded file reaches clients with a warm cache.
			for _, asset := range tc.vendor {
				needle := "/ui/" + asset + "?v="
				if !strings.Contains(body, needle) {
					t.Errorf("the page does not load %q", needle)
				}
			}
		})
	}
}

// TestUIRoute_VendoredLibrariesServed checks that the files the pages reference
// exist, are served with the right content type and get the one-year cache the
// pages' ?v= hash makes safe.
func TestUIRoute_VendoredLibrariesServed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Chdir("../../..")

	r := gin.New()
	NewUIRouter(r, GhostboardPageConfig{MaxTextBytes: 16777216, MaxFileBytes: 838860800})

	cases := []struct {
		name        string
		asset       string
		contentType string
		body        string
	}{
		{name: "alpine", asset: vendorAlpineJS, contentType: "javascript", body: "Alpine"},
		{name: "alpine collapse", asset: vendorAlpineCollapse, contentType: "javascript", body: "collapse"},
		{name: "htmx", asset: vendorHtmx, contentType: "javascript", body: "htmx"},
		{name: "tailwind", asset: vendorTailwind, contentType: "text/css", body: "tailwindcss"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ui/"+tc.asset, nil))

			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", w.Code)
			}
			if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, tc.contentType) {
				t.Errorf("Content-Type = %q, want it to contain %q", ct, tc.contentType)
			}
			if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "max-age=31536000") {
				t.Errorf("Cache-Control = %q, want the long-lived asset cache", cc)
			}
			if body := w.Body.String(); !strings.Contains(body, tc.body) {
				t.Errorf("the served body does not look like %s", tc.name)
			}
		})
	}
}

// TestUIRoute_ServiceWorkerPrecacheFilesExist guards the offline install: a
// single missing path makes cache.addAll reject, so the worker never installs
// and the pages stop working offline without any visible error.
func TestUIRoute_ServiceWorkerPrecacheFilesExist(t *testing.T) {
	t.Chdir("../../..")

	body, err := os.ReadFile(filepath.Join("ui", "service-worker.js"))
	if err != nil {
		t.Fatalf("cannot read the service worker: %v", err)
	}

	// The precache list also holds the page routes, which are files here.
	routes := map[string]string{
		"/ui":            "index.html",
		"/ui/":           "index.html",
		"/ui/home":       "home.html",
		"/ui/ghostboard": "ghostboard.html",
	}

	// Only the install list matters here; the rest of the worker also quotes
	// /ui paths, but those are route comparisons rather than files to cache.
	source := string(body)
	start := strings.Index(source, "cache.addAll([")
	end := strings.Index(source, "])")
	if start < 0 || end < start {
		t.Fatal("cannot find the cache.addAll list in the service worker")
	}

	entries := regexp.MustCompile(`'(/ui[^']*)'`).FindAllStringSubmatch(source[start:end], -1)
	if len(entries) == 0 {
		t.Fatal("no precached path found: the service worker no longer pre-caches the UI")
	}

	for _, entry := range entries {
		path := entry[1]
		if file, ok := routes[path]; ok {
			path = file
		} else {
			path = strings.TrimPrefix(path, "/ui/")
		}
		if _, err := os.Stat(filepath.Join("ui", path)); err != nil {
			t.Errorf("the service worker pre-caches %q, but ui/%s does not exist: the install would fail and the offline cache would never be filled", entry[1], path)
		}
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
