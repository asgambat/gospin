package boardclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asgambat/gospin/internal/ghostboard"
)

// fakeFileServer mimics the plain attachment API of the server.
type fakeFileServer struct {
	names       map[string]string
	files       map[string][]byte
	token       string
	notFoundRaw string
	mu          sync.Mutex
	unlocks     int
	uploads     int
	deletes     int
	// unlockStatus, when set, makes the unlock endpoint answer that status.
	unlockStatus int
	requirePIN   bool
}

func newFakeFileServer(t *testing.T) (*fakeFileServer, *httptest.Server) {
	t.Helper()

	fake := &fakeFileServer{
		names: make(map[string]string),
		files: make(map[string][]byte),
		token: "session-token",
	}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, server
}

func (f *fakeFileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if strings.HasSuffix(r.URL.Path, "/unlock") && r.Method == http.MethodPost {
		f.unlocks++
		w.Header().Set("Content-Type", "application/json")
		if f.unlockStatus != 0 {
			w.WriteHeader(f.unlockStatus)
			_, _ = w.Write([]byte(`{"error":"board is not protected by a pin"}`))
			return
		}
		_, _ = w.Write([]byte(`{"token":"` + f.token + `"}`))
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/boards/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[1] != "files" {
		http.NotFound(w, r)
		return
	}
	board := parts[0]

	if f.requirePIN && r.Header.Get("Authorization") != "Bearer "+f.token {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"` + ghostboard.ErrPINRequired.Error() + `","board":"` + board + `"}`))
		return
	}

	if f.notFoundRaw != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(f.notFoundRaw))
		return
	}

	switch {
	case len(parts) == 2 && r.Method == http.MethodGet:
		var body strings.Builder
		for id := range f.files {
			body.WriteString(id + "\t" + f.names[id] + "\t" + itoa(len(f.files[id])) + "\t2026-10-30T00:00:00Z\n")
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(body.String()))
	case len(parts) == 2 && r.Method == http.MethodPost:
		reader, err := r.MultipartReader()
		if err != nil {
			http.Error(w, "not multipart", http.StatusBadRequest)
			return
		}
		part, err := reader.NextPart()
		if err != nil {
			http.Error(w, "no part", http.StatusBadRequest)
			return
		}
		content := make([]byte, 64)
		read, err := part.Read(content)
		if err != nil && read == 0 {
			http.Error(w, "empty part", http.StatusBadRequest)
			return
		}
		f.uploads++
		id := "aaaaaaaaaaaaaaaaaaaaaaaaaa"
		f.files[id] = content[:read]
		f.names[id] = part.FileName()

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(id + "\t" + part.FileName() + "\t" + itoa(read) + "\t2026-10-30T00:00:00Z\n"))
	case len(parts) == 3 && r.Method == http.MethodGet:
		id := parts[2]
		data, ok := f.files[id]
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"file not found"}`))
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(data)
	case len(parts) == 3 && r.Method == http.MethodDelete:
		id := parts[2]
		if _, ok := f.files[id]; !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"file not found"}`))
			return
		}
		delete(f.files, id)
		delete(f.names, id)
		f.deletes++
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func itoa(value int) string {
	return strconv.Itoa(value)
}

func newFileClient(t *testing.T, url string, opts Options) *Client {
	t.Helper()

	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}
	client, err := NewWithOptions(url, opts)
	if err != nil {
		t.Fatalf("NewWithOptions() error = %v", err)
	}
	return client
}

func TestClientListFiles(t *testing.T) {
	fake, server := newFakeFileServer(t)
	fake.files["aaaaaaaaaaaaaaaaaaaaaaaaaa"] = []byte("hello")
	fake.names["aaaaaaaaaaaaaaaaaaaaaaaaaa"] = "hello.txt"

	client := newFileClient(t, server.URL, Options{})
	files, err := client.ListFiles(context.Background(), "team")
	if err != nil {
		t.Fatalf("ListFiles() error = %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("ListFiles() = %d files, want 1", len(files))
	}
	if files[0].Name != "hello.txt" || files[0].Size != 5 {
		t.Errorf("file = %+v, want hello.txt of 5 bytes", files[0])
	}
	if files[0].ExpiresAt == nil || files[0].ExpiresAt.IsZero() {
		t.Error("ExpiresAt is missing from the parsed listing")
	}
}

func TestClientUploadFile(t *testing.T) {
	fake, server := newFakeFileServer(t)

	client := newFileClient(t, server.URL, Options{})
	file, err := client.UploadFile(context.Background(), "team", "note.txt", strings.NewReader("streamed content"))
	if err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	if file.Name != "note.txt" || file.Size != 16 {
		t.Errorf("UploadFile() = %+v, want note.txt of 16 bytes", file)
	}
	if fake.names[file.ID] != "note.txt" {
		t.Errorf("server received the name %q, want note.txt", fake.names[file.ID])
	}
	if fake.uploads != 1 {
		t.Errorf("upload calls = %d, want 1", fake.uploads)
	}
}

func TestClientDownloadFile(t *testing.T) {
	fake, server := newFakeFileServer(t)
	id := "aaaaaaaaaaaaaaaaaaaaaaaaaa"
	fake.files[id] = []byte("downloaded")
	fake.names[id] = "a.txt"

	client := newFileClient(t, server.URL, Options{})
	var out strings.Builder
	written, err := client.DownloadFile(context.Background(), "team", id, &out)
	if err != nil {
		t.Fatalf("DownloadFile() error = %v", err)
	}
	if written != 10 || out.String() != "downloaded" {
		t.Errorf("DownloadFile() = %d byte(s) %q, want the stored bytes", written, out.String())
	}
}

func TestClientDownloadFileNotFound(t *testing.T) {
	_, server := newFakeFileServer(t)
	client := newFileClient(t, server.URL, Options{})

	_, err := client.DownloadFile(context.Background(), "team", "aaaaaaaaaaaaaaaaaaaaaaaaaa", &strings.Builder{})
	if !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("DownloadFile() error = %v, want ErrFileNotFound", err)
	}
}

func TestClientDownloadFileMissingRoute(t *testing.T) {
	fake, server := newFakeFileServer(t)
	fake.notFoundRaw = `{"error":"not found"}`
	client := newFileClient(t, server.URL, Options{})

	_, err := client.DownloadFile(context.Background(), "team", "aaaaaaaaaaaaaaaaaaaaaaaaaa", &strings.Builder{})
	var endpointErr *EndpointError
	if !errors.As(err, &endpointErr) {
		t.Fatalf("DownloadFile() error = %v, want EndpointError for a missing route", err)
	}
}

func TestClientDeleteFile(t *testing.T) {
	fake, server := newFakeFileServer(t)
	id := "aaaaaaaaaaaaaaaaaaaaaaaaaa"
	fake.files[id] = []byte("bye")
	fake.names[id] = "a.txt"

	client := newFileClient(t, server.URL, Options{})
	if err := client.DeleteFile(context.Background(), "team", id); err != nil {
		t.Fatalf("DeleteFile() error = %v", err)
	}
	if _, ok := fake.files[id]; ok {
		t.Error("the file is still on the server")
	}
	if fake.deletes != 1 {
		t.Errorf("delete calls = %d, want 1", fake.deletes)
	}
}

func TestClientDeleteFileNotFound(t *testing.T) {
	_, server := newFakeFileServer(t)
	client := newFileClient(t, server.URL, Options{})

	err := client.DeleteFile(context.Background(), "team", "aaaaaaaaaaaaaaaaaaaaaaaaaa")
	if !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("DeleteFile() error = %v, want ErrFileNotFound", err)
	}
}

func TestClientDeleteFileMissingRoute(t *testing.T) {
	fake, server := newFakeFileServer(t)
	fake.notFoundRaw = `{"error":"not found"}`
	client := newFileClient(t, server.URL, Options{})

	err := client.DeleteFile(context.Background(), "team", "aaaaaaaaaaaaaaaaaaaaaaaaaa")
	var endpointErr *EndpointError
	if !errors.As(err, &endpointErr) {
		t.Fatalf("DeleteFile() error = %v, want EndpointError for a missing route", err)
	}
}

func TestClientDeleteFileUnlocksFirst(t *testing.T) {
	fake, server := newFakeFileServer(t)
	fake.requirePIN = true
	id := "aaaaaaaaaaaaaaaaaaaaaaaaaa"
	fake.files[id] = []byte("secret")

	client := newFileClient(t, server.URL, Options{PIN: "1234"})
	if err := client.DeleteFile(context.Background(), "lavoro", id); err != nil {
		t.Fatalf("DeleteFile() error = %v", err)
	}
	if fake.unlocks != 1 {
		t.Errorf("unlock calls = %d, want 1 before the delete", fake.unlocks)
	}
	if fake.deletes != 1 {
		t.Errorf("delete calls = %d, want 1", fake.deletes)
	}
}

func TestClientDeleteFileEmptyID(t *testing.T) {
	_, server := newFakeFileServer(t)
	client := newFileClient(t, server.URL, Options{})

	if err := client.DeleteFile(context.Background(), "team", "  "); err == nil {
		t.Fatal("DeleteFile() with an empty id = nil error, want a failure")
	}
}

func TestClientUploadUnlocksBeforeStreaming(t *testing.T) {
	fake, server := newFakeFileServer(t)
	fake.requirePIN = true

	client := newFileClient(t, server.URL, Options{PIN: "1234"})
	if _, err := client.UploadFile(context.Background(), "lavoro", "a.txt", strings.NewReader("secret")); err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	if fake.unlocks != 1 {
		t.Errorf("unlock calls = %d, want 1 before the upload", fake.unlocks)
	}
	if fake.uploads != 1 {
		t.Errorf("upload calls = %d, want 1", fake.uploads)
	}
}

func TestClientUploadToPublicBoardIgnoresUnlock404(t *testing.T) {
	fake, server := newFakeFileServer(t)
	// A public board answers 404 to an unlock: the client must still upload.
	fake.unlockStatus = http.StatusNotFound

	client := newFileClient(t, server.URL, Options{PIN: "1234"})
	if _, err := client.UploadFile(context.Background(), "team", "a.txt", strings.NewReader("x")); err != nil {
		t.Fatalf("UploadFile() error = %v, want the upload to proceed after a 404 unlock", err)
	}
	if fake.uploads != 1 {
		t.Errorf("upload calls = %d, want 1", fake.uploads)
	}
}
