package controller

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/asgambat/gospin/internal/ghostboard"
	"github.com/asgambat/gospin/internal/logger"
)

// newFileTestController builds a controller, its router and the store behind
// them, with attachments enabled in a temporary directory.
func newFileTestController(t *testing.T, tune func(*ghostboard.FileConfig)) (*GhostboardController, *gin.Engine, *ghostboard.Store) {
	t.Helper()

	dir := t.TempDir()
	files := ghostboard.FileConfig{
		Enabled:         true,
		Root:            filepath.Join(dir, "files"),
		MaxFileBytes:    1 << 20,
		MaxBoardBytes:   4 << 20,
		MaxTotalBytes:   8 << 20,
		MaxFileCount:    5,
		TTL:             time.Hour,
		TransferTimeout: time.Minute,
	}
	if tune != nil {
		tune(&files)
	}

	store, err := ghostboard.New(ghostboard.Config{
		FilePath:        filepath.Join(dir, "ghostboard.json"),
		ClearDelay:      time.Minute,
		MaxTextBytes:    1024,
		PersistInterval: time.Second,
		Files:           files,
	})
	if err != nil {
		t.Fatalf("ghostboard.New() error = %v", err)
	}

	gc := NewGhostboardController(store, nil, 1024, 20*time.Millisecond)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ghostboard/:board/files", gc.ListFiles)
	r.POST("/ghostboard/:board/files", gc.UploadFile)
	r.GET("/ghostboard/:board/files/:id", gc.DownloadFile)
	r.DELETE("/ghostboard/:board/files/:id", gc.DeleteFile)
	r.DELETE("/ghostboard/:board/files", gc.DeleteFiles)
	r.GET("/api/boards/:board/files", gc.ListFilesPlain)
	r.POST("/api/boards/:board/files", gc.UploadFilePlain)
	return gc, r, store
}

// multipartBody builds a one-part upload. An empty filename produces a plain
// field named after the form field, which is how a client sends a part without
// a file.
func multipartBody(t *testing.T, field, filename, content string) (*bytes.Buffer, string) {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	var (
		part io.Writer
		err  error
	)
	if filename == "" {
		part, err = writer.CreateFormField(field)
	} else {
		part, err = writer.CreateFormFile(field, filename)
	}
	if err != nil {
		t.Fatalf("creating the multipart part: %v", err)
	}
	if _, err := io.WriteString(part, content); err != nil {
		t.Fatalf("writing the multipart part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing the multipart writer: %v", err)
	}
	return &body, writer.FormDataContentType()
}

func uploadFile(t *testing.T, r *gin.Engine, path, filename, content string) *httptest.ResponseRecorder {
	t.Helper()

	body, contentType := multipartBody(t, "file", filename, content)
	req := httptest.NewRequest(http.MethodPost, path, body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestGhostboardFileLifecycle(t *testing.T) {
	_, r, _ := newFileTestController(t, nil)

	w := uploadFile(t, r, "/ghostboard/team/files", "report.txt", "hello attachment")
	if w.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201: %s", w.Code, w.Body.String())
	}

	var stored ghostboard.File
	if err := json.Unmarshal(w.Body.Bytes(), &stored); err != nil {
		t.Fatalf("decoding the upload answer: %v", err)
	}
	if stored.Name != "report.txt" || stored.Size != 16 {
		t.Errorf("stored = %+v, want report.txt of 16 bytes", stored)
	}
	if loc := w.Header().Get("Location"); loc != "/ghostboard/team/files/"+stored.ID {
		t.Errorf("Location = %q, want the URL of the new file", loc)
	}

	// The board payload carries the attachments with the text.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ghostboard/team/files", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", w.Code)
	}
	var listing struct {
		Files []ghostboard.File `json:"files"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listing); err != nil {
		t.Fatalf("decoding the listing: %v", err)
	}
	if len(listing.Files) != 1 || listing.Files[0].ID != stored.ID {
		t.Fatalf("listing = %+v, want the uploaded file", listing.Files)
	}

	// A download is always an attachment: never inline, never sniffed.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ghostboard/team/files/"+stored.ID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("download status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); body != "hello attachment" {
		t.Errorf("download body = %q, want the uploaded bytes", body)
	}
	if got := w.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", got)
	}
	if got := w.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Errorf("Content-Disposition = %q, want an attachment", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := w.Header().Get("Content-Security-Policy"); got != "sandbox" {
		t.Errorf("Content-Security-Policy = %q, want sandbox", got)
	}

	// Delete removes the file and a second delete answers 404.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/ghostboard/team/files/"+stored.ID, nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/ghostboard/team/files/"+stored.ID, nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("second delete status = %d, want 404", w.Code)
	}
}

func TestGhostboardFileDeleteAll(t *testing.T) {
	_, r, _ := newFileTestController(t, nil)

	uploadFile(t, r, "/ghostboard/team/files", "a.txt", "a")
	uploadFile(t, r, "/ghostboard/team/files", "b.txt", "b")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/ghostboard/team/files", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete all status = %d, want 204: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/boards/team/files", nil))
	if body := w.Body.String(); body != "" {
		t.Errorf("plain listing after the delete = %q, want empty", body)
	}
}

func TestGhostboardUploadRejections(t *testing.T) {
	_, r, _ := newFileTestController(t, func(f *ghostboard.FileConfig) {
		f.MaxFileBytes = 4
		f.MaxBoardBytes = 6
		f.MaxFileCount = 2
	})

	t.Run("wrong content type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/ghostboard/team/files", strings.NewReader("plain"))
		req.Header.Set("Content-Type", "text/plain")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("status = %d, want 415: %s", w.Code, w.Body.String())
		}
	})

	t.Run("no file part", func(t *testing.T) {
		body, contentType := multipartBody(t, "note", "", "x")
		req := httptest.NewRequest(http.MethodPost, "/ghostboard/team/files", body)
		req.Header.Set("Content-Type", contentType)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
		}
	})

	t.Run("part without a filename", func(t *testing.T) {
		w := uploadFile(t, r, "/ghostboard/team/files", "", "x")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
		}
	})

	t.Run("empty file", func(t *testing.T) {
		w := uploadFile(t, r, "/ghostboard/team/files", "empty.txt", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
		}
	})

	t.Run("too large", func(t *testing.T) {
		w := uploadFile(t, r, "/ghostboard/team/files", "big.txt", "12345")
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413: %s", w.Code, w.Body.String())
		}
		var payload struct {
			Limit      string `json:"limit"`
			LimitBytes int64  `json:"limit_bytes"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decoding the error: %v", err)
		}
		if payload.Limit != "max_file_bytes" || payload.LimitBytes != 4 {
			t.Errorf("error body = %+v, want max_file_bytes of 4", payload)
		}
	})

	t.Run("board quota", func(t *testing.T) {
		_, r, _ := newFileTestController(t, func(f *ghostboard.FileConfig) {
			f.MaxFileBytes = 4
			f.MaxBoardBytes = 6
		})
		if w := uploadFile(t, r, "/ghostboard/team/files", "a.txt", "1234"); w.Code != http.StatusCreated {
			t.Fatalf("first upload status = %d, want 201: %s", w.Code, w.Body.String())
		}
		w := uploadFile(t, r, "/ghostboard/team/files", "b.txt", "1234")
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("second upload status = %d, want 413: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "max_board_file_bytes") {
			t.Errorf("error body %s does not name the board limit", w.Body.String())
		}
	})

	t.Run("file count", func(t *testing.T) {
		_, r, _ := newFileTestController(t, func(f *ghostboard.FileConfig) { f.MaxFileCount = 1 })
		if w := uploadFile(t, r, "/ghostboard/team/files", "a.txt", "a"); w.Code != http.StatusCreated {
			t.Fatalf("first upload status = %d, want 201: %s", w.Code, w.Body.String())
		}
		w := uploadFile(t, r, "/ghostboard/team/files", "b.txt", "b")
		if w.Code != http.StatusConflict {
			t.Fatalf("second upload status = %d, want 409: %s", w.Code, w.Body.String())
		}
		var payload struct {
			Limit      string `json:"limit"`
			LimitValue int    `json:"limit_value"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decoding the error: %v", err)
		}
		if payload.Limit != "max_file_count" || payload.LimitValue != 1 {
			t.Errorf("error body = %+v, want max_file_count of 1", payload)
		}
	})
}

func TestGhostboardRejectionsAreLogged(t *testing.T) {
	_, r, _ := newFileTestController(t, func(f *ghostboard.FileConfig) { f.MaxFileCount = 1 })

	var logs bytes.Buffer
	original := logger.Logger.Out
	level := logger.Logger.GetLevel()
	logger.Logger.SetOutput(&logs)
	logger.Logger.SetLevel(logrus.WarnLevel)
	t.Cleanup(func() {
		logger.Logger.SetOutput(original)
		logger.Logger.SetLevel(level)
	})

	uploadFile(t, r, "/ghostboard/team/files", "a.txt", "a")
	if w := uploadFile(t, r, "/ghostboard/team/files", "b.txt", "b"); w.Code != http.StatusConflict {
		t.Fatalf("second upload status = %d, want 409: %s", w.Code, w.Body.String())
	}

	// The refusal names the board and the limit that was hit, which is what an
	// operator needs to see; the page only shows the message to the client.
	logged := logs.String()
	if !strings.Contains(logged, "level=warning") || !strings.Contains(logged, "team") {
		t.Errorf("the refusal was not logged at warn with the board: %q", logged)
	}
	if !strings.Contains(logged, "max_file_count") {
		t.Errorf("the log line does not say which limit was hit: %q", logged)
	}
}

func TestGhostboardDownloadRangesAndNames(t *testing.T) {
	_, r, _ := newFileTestController(t, nil)

	w := uploadFile(t, r, "/ghostboard/team/files", "caffè.txt", "0123456789")
	if w.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201: %s", w.Code, w.Body.String())
	}
	var stored ghostboard.File
	if err := json.Unmarshal(w.Body.Bytes(), &stored); err != nil {
		t.Fatalf("decoding the upload answer: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ghostboard/team/files/"+stored.ID, nil)
	req.Header.Set("Range", "bytes=2-4")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusPartialContent {
		t.Fatalf("range status = %d, want 206: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); body != "234" {
		t.Errorf("range body = %q, want 234", body)
	}

	disposition := w.Header().Get("Content-Disposition")
	if !strings.Contains(disposition, "filename*=UTF-8''caff%C3%A8.txt") {
		t.Errorf("Content-Disposition = %q, want the RFC 5987 name", disposition)
	}
	if !strings.Contains(disposition, `filename="caff_.txt"`) {
		t.Errorf("Content-Disposition = %q, want an ASCII fallback", disposition)
	}
}

func TestGhostboardDownloadUnknownId(t *testing.T) {
	_, r, _ := newFileTestController(t, nil)

	tests := []struct {
		name string
		id   string
		want int
	}{
		{name: "invalid", id: "nope", want: http.StatusBadRequest},
		{name: "unknown", id: "aaaaaaaaaaaaaaaaaaaaaaaaaa", want: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ghostboard/team/files/"+tt.id, nil))
			if w.Code != tt.want {
				t.Errorf("status = %d, want %d: %s", w.Code, tt.want, w.Body.String())
			}
		})
	}
}

func TestGhostboardFileRoutesDisabled(t *testing.T) {
	_, r, _ := newFileTestController(t, func(f *ghostboard.FileConfig) { f.Enabled = false })

	// The store is disabled, so the controller answers 404 even though the
	// routes exist in this test router.
	w := uploadFile(t, r, "/ghostboard/team/files", "a.txt", "a")
	if w.Code != http.StatusNotFound {
		t.Errorf("upload status = %d, want 404", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ghostboard/team/files", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("list status = %d, want 404", w.Code)
	}
}

func TestGhostboardPlainFileSurfaces(t *testing.T) {
	_, r, _ := newFileTestController(t, nil)

	w := uploadFile(t, r, "/api/boards/team/files", "note.txt", "plain listing")
	if w.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201: %s", w.Code, w.Body.String())
	}
	if contentType := w.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", contentType)
	}

	line := strings.TrimRight(w.Body.String(), "\n")
	fields := strings.Split(line, "\t")
	if len(fields) != 4 {
		t.Fatalf("upload line = %q, want four tab-separated fields", line)
	}
	if fields[1] != "note.txt" || fields[2] != "13" {
		t.Errorf("upload line = %q, want the name and size", line)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/boards/team/files", nil))
	if got := strings.Count(w.Body.String(), "\n"); got != 1 {
		t.Errorf("the plain listing has %d lines, want 1: %q", got, w.Body.String())
	}
}

func TestFilenameLineCannotForgeFields(t *testing.T) {
	got := filenameLine("a\tb\r\nc.txt")
	if got != "a b  c.txt" {
		t.Errorf("filenameLine() = %q, want the separators replaced", got)
	}
}

func TestGhostboardContentDispositionSanitizes(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "plain.txt", want: `filename="plain.txt"`},
		{name: "quote\"name.txt", want: `filename="quote_name.txt"`},
		{name: "line\nbreak", want: `filename="line_break"`},
	}

	for _, tt := range tests {
		got := ghostboardContentDisposition(tt.name)
		if !strings.Contains(got, tt.want) {
			t.Errorf("ghostboardContentDisposition(%q) = %q, want %s", tt.name, got, tt.want)
		}
		if strings.ContainsAny(got, "\r\n") {
			t.Errorf("ghostboardContentDisposition(%q) = %q, want no line break", tt.name, got)
		}
	}
}
