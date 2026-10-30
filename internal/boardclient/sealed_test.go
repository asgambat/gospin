package boardclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/asgambat/gospin/internal/sealedcrypto"
)

// sealedFake is a tiny board API that stores one envelope per board and records
// the headers of the last write.
type sealedFake struct {
	boards  map[string]string
	headers http.Header
	last    string
}

func newSealedFake(t *testing.T) (*sealedFake, *httptest.Server) {
	t.Helper()

	fake := &sealedFake{boards: make(map[string]string)}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, server
}

func (f *sealedFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	board := strings.TrimPrefix(r.URL.Path, "/api/boards/")
	board = strings.TrimPrefix(board, "/ghostboard/")

	if strings.HasSuffix(board, "/open") {
		board = strings.TrimSuffix(board, "/open")
		envelope, ok := f.boards[board]
		if !ok {
			w.WriteHeader(http.StatusGone)
			_, _ = io.WriteString(w, `{"error":"board was burned"}`)
			return
		}
		delete(f.boards, board)
		f.last = "open"
		_, _ = io.WriteString(w, envelope)
		return
	}

	switch r.Method {
	case http.MethodPost:
		body, _ := io.ReadAll(r.Body)
		f.headers = r.Header.Clone()
		f.boards[board] = string(body)
		f.last = "write"
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		envelope, ok := f.boards[board]
		if !ok {
			envelope = ""
		}
		f.last = "read"
		if strings.HasPrefix(r.URL.Path, "/ghostboard/") {
			payload, _ := json.Marshal(map[string]any{
				"name":     board,
				"text":     envelope,
				"mode":     "sealed",
				"revision": 1,
			})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(payload)
			return
		}
		_, _ = io.WriteString(w, envelope)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func TestSealedWriteSealsBeforeItSends(t *testing.T) {
	fake, server := newSealedFake(t)
	client, err := New(server.URL, time.Second)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	key, err := sealedcrypto.NewKey()
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}

	if err := client.SealedWrite(context.Background(), "nota", "il testo segreto", key, true); err != nil {
		t.Fatalf("SealedWrite() error = %v", err)
	}

	if got := fake.headers.Get(sealedModeHeader); got != sealedModeValue {
		t.Fatalf("%s = %q, want %q", sealedModeHeader, got, sealedModeValue)
	}
	if got := fake.headers.Get(sealedBurnHeader); got != "true" {
		t.Fatalf("%s = %q, want true", sealedBurnHeader, got)
	}

	envelope := fake.boards["nota"]
	if strings.Contains(envelope, "il testo segreto") {
		t.Fatal("the plaintext reached the server")
	}
	plaintext, err := sealedcrypto.Open(key, "nota", envelope)
	if err != nil {
		t.Fatalf("the stored envelope does not open: %v", err)
	}
	if string(plaintext) != "il testo segreto" {
		t.Fatalf("the envelope decrypts to %q", plaintext)
	}
}

func TestSealedReadDecryptsTheEnvelope(t *testing.T) {
	fake, server := newSealedFake(t)
	client, err := New(server.URL, time.Second)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	key, err := sealedcrypto.NewKey()
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	envelope, err := sealedcrypto.Seal(key, "nota", []byte("testo"))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	fake.boards["nota"] = envelope

	text, err := client.SealedRead(context.Background(), "nota", key)
	if err != nil {
		t.Fatalf("SealedRead() error = %v", err)
	}
	if text != "testo" {
		t.Fatalf("SealedRead() = %q, want the plaintext", text)
	}

	other, err := sealedcrypto.NewKey()
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	if _, err := client.SealedRead(context.Background(), "nota", other); !errors.Is(err, sealedcrypto.ErrDecrypt) {
		t.Fatalf("SealedRead() with a wrong key returned %v, want a key error", err)
	}

	var keyErr *SealedKeyError
	if _, err := client.SealedRead(context.Background(), "nota", other); !errors.As(err, &keyErr) {
		t.Fatalf("the wrong key did not produce a SealedKeyError: %v", err)
	}
}

func TestSealedGetBoardDecryptsTheState(t *testing.T) {
	fake, server := newSealedFake(t)
	client, err := New(server.URL, time.Second)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	key, err := sealedcrypto.NewKey()
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	envelope, err := sealedcrypto.Seal(key, "nota", []byte("con revisione"))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	fake.boards["nota"] = envelope

	state, err := client.SealedGetBoard(context.Background(), "nota", key)
	if err != nil {
		t.Fatalf("SealedGetBoard() error = %v", err)
	}
	if state.Text != "con revisione" || !state.Sealed() || state.Revision != 1 {
		t.Fatalf("unexpected state: %+v", state)
	}
}

func TestSealedOpenConsumesTheBoard(t *testing.T) {
	fake, server := newSealedFake(t)
	client, err := New(server.URL, time.Second)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	key, err := sealedcrypto.NewKey()
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	envelope, err := sealedcrypto.Seal(key, "nota", []byte("una volta"))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	fake.boards["nota"] = envelope

	text, err := client.SealedOpen(context.Background(), "nota", key)
	if err != nil {
		t.Fatalf("SealedOpen() error = %v", err)
	}
	if text != "una volta" {
		t.Fatalf("SealedOpen() = %q", text)
	}
	if fake.last != "open" {
		t.Fatalf("the last request was %q, want open", fake.last)
	}

	if _, err := client.SealedOpen(context.Background(), "nota", key); err == nil {
		t.Fatal("the second open succeeded on a burned board")
	} else {
		var responseErr *ResponseError
		if !errors.As(err, &responseErr) || responseErr.Status != http.StatusGone {
			t.Fatalf("the second open returned %v, want a 410", err)
		}
	}
}
