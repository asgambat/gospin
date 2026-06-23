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
