package route

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/asgambat/gospin/internal/app"
	"github.com/asgambat/gospin/internal/config"
	"github.com/asgambat/gospin/internal/ghostboard"
)

// newFileRouteApp builds an application whose ghostboard accepts attachments,
// optionally behind the shared PIN.
func newFileRouteApp(t *testing.T, filesEnabled, private bool) *app.App {
	t.Helper()

	dir := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:               8084,
			RequestTimeout:     500 * time.Millisecond,
			CORSAllowedOrigins: "*",
			ReadTimeout:        30 * time.Second,
			WriteTimeout:       30 * time.Second,
		},
		Ghostboard: config.GhostboardConfig{
			FilePath:            filepath.Join(dir, "ghostboard.json"),
			ClearDelay:          time.Minute,
			MaxTextBytes:        1024,
			PersistInterval:     time.Second,
			HeartbeatInterval:   time.Second,
			FilesEnabled:        filesEnabled,
			MaxFileBytes:        4096,
			MaxFileCount:        5,
			MaxBoardFileBytes:   8192,
			MaxTotalFileBytes:   16384,
			FileTTL:             time.Hour,
			FileTransferTimeout: time.Minute,
		},
	}

	files := ghostboard.FileConfig{}
	if filesEnabled {
		files = ghostboard.FileConfig{
			Enabled:         true,
			Root:            filepath.Join(dir, "files"),
			MaxFileBytes:    4096,
			MaxBoardBytes:   8192,
			MaxTotalBytes:   16384,
			MaxFileCount:    5,
			TTL:             time.Hour,
			TransferTimeout: time.Minute,
		}
	}

	store, err := ghostboard.New(ghostboard.Config{
		FilePath:        cfg.Ghostboard.FilePath,
		ClearDelay:      cfg.Ghostboard.ClearDelay,
		MaxTextBytes:    cfg.Ghostboard.MaxTextBytes,
		PersistInterval: cfg.Ghostboard.PersistInterval,
		Files:           files,
	})
	if err != nil {
		t.Fatalf("ghostboard.New() error = %v", err)
	}

	var auth *ghostboard.Authenticator
	if private {
		digest, err := ghostboard.PINDigest("0123456789abcdef", "1234")
		if err != nil {
			t.Fatalf("PINDigest() error = %v", err)
		}
		auth, err = ghostboard.NewAuthenticator(ghostboard.AuthConfig{
			PINDigest:     digest,
			Secret:        "0123456789abcdef",
			PrivateBoards: []string{"lavoro"},
			SessionTTL:    time.Hour,
			MaxAttempts:   3,
			Lockout:       time.Minute,
		})
		if err != nil {
			t.Fatalf("NewAuthenticator() error = %v", err)
		}
	}

	return &app.App{
		Config:         cfg,
		Cache:          &stubStore{},
		Runtime:        &stubRuntime{},
		Ghostboard:     store,
		GhostboardAuth: auth,
		BaseCtx:        context.Background(),
	}
}

// routeUpload posts one multipart file to path.
func routeUpload(t *testing.T, r *gin.Engine, path, token, filename, content string) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("creating the part: %v", err)
	}
	if _, err := io.WriteString(part, content); err != nil {
		t.Fatalf("writing the part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing the writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestGhostboardFileRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	r := SetupRoutes(newFileRouteApp(t, true, false), logger)

	w := routeUpload(t, r, "/ghostboard/team/files", "", "a.txt", "content")
	if w.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201: %s", w.Code, w.Body.String())
	}

	var stored ghostboard.File
	if err := json.Unmarshal(w.Body.Bytes(), &stored); err != nil {
		t.Fatalf("decoding the upload answer: %v", err)
	}

	for _, path := range []string{
		"/ghostboard/team/files",
		"/api/boards/team/files",
		"/ghostboard/team/files/" + stored.ID,
		"/api/boards/team/files/" + stored.ID,
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200: %s", path, w.Code, w.Body.String())
		}
	}

	// A download is never compressed: gzip would strip the Content-Length and
	// break Range on a large file.
	download := httptest.NewRequest(http.MethodGet, "/ghostboard/team/files/"+stored.ID, nil)
	download.Header.Set("Accept-Encoding", "gzip")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, download)
	if encoding := w.Header().Get("Content-Encoding"); encoding != "" {
		t.Errorf("Content-Encoding = %q, want no compression on a download", encoding)
	}
	if w.Header().Get("Content-Length") == "" {
		t.Error("Content-Length is missing from the download")
	}

	// The plain surface accepts an upload too.
	if w := routeUpload(t, r, "/api/boards/team/files", "", "b.txt", "plain"); w.Code != http.StatusCreated {
		t.Errorf("plain upload status = %d, want 201: %s", w.Code, w.Body.String())
	}
}

func TestGhostboardFileRoutesNeedThePIN(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	r := SetupRoutes(newFileRouteApp(t, true, true), logger)

	paths := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/ghostboard/lavoro/files"},
		{method: http.MethodGet, path: "/api/boards/lavoro/files"},
		{method: http.MethodGet, path: "/ghostboard/lavoro/files/aaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{method: http.MethodDelete, path: "/ghostboard/lavoro/files/aaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{method: http.MethodDelete, path: "/ghostboard/lavoro/files"},
	}

	for _, tt := range paths {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tt.method, tt.path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401: %s", tt.method, tt.path, w.Code, w.Body.String())
		}
	}

	if w := routeUpload(t, r, "/ghostboard/lavoro/files", "", "a.txt", "x"); w.Code != http.StatusUnauthorized {
		t.Errorf("upload without a session = %d, want 401: %s", w.Code, w.Body.String())
	}

	// A public board on the same server needs no session.
	if w := routeUpload(t, r, "/ghostboard/team/files", "", "a.txt", "x"); w.Code != http.StatusCreated {
		t.Fatalf("upload to a public board = %d, want 201: %s", w.Code, w.Body.String())
	}

	// The PIN buys a session that the file routes accept.
	unlock := httptest.NewRequest(http.MethodPost, "/ghostboard/lavoro/unlock", bytes.NewReader([]byte(`{"pin":"1234"}`)))
	unlock.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, unlock)
	if w.Code != http.StatusOK {
		t.Fatalf("unlock status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var session struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatalf("decoding the session: %v", err)
	}

	if w := routeUpload(t, r, "/ghostboard/lavoro/files", session.Token, "a.txt", "secret"); w.Code != http.StatusCreated {
		t.Fatalf("upload with a session = %d, want 201: %s", w.Code, w.Body.String())
	}
}

func TestGhostboardFileRoutesAbsentWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	r := SetupRoutes(newFileRouteApp(t, false, false), logger)

	for _, path := range []string{"/ghostboard/team/files", "/api/boards/team/files"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404 when attachments are disabled", path, w.Code)
		}
	}

	if w := routeUpload(t, r, "/ghostboard/team/files", "", "a.txt", "x"); w.Code != http.StatusNotFound {
		t.Errorf("upload with attachments disabled = %d, want 404", w.Code)
	}
}
