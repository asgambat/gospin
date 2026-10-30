package sealedcrypto

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// vectorPath is the shared known-answer vector. It lives under ui/assets because
// the page will load the very same file when its WebCrypto implementation lands,
// so the Go implementation and the browser one are pinned to one file.
const vectorPath = "../../ui/assets/sealed-vector.json"

// vectorFile is the on-disk shape of the vector.
type vectorFile struct {
	Board     string `json:"board"`
	Key       string `json:"key"`
	IV        string `json:"iv"`
	Plaintext string `json:"plaintext"`
	Envelope  string `json:"envelope"`
	Version   int    `json:"version"`
}

func loadVector(t *testing.T) vectorFile {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(vectorPath))
	if err != nil {
		t.Fatalf("reading the known-answer vector %s: %v", vectorPath, err)
	}

	var vector vectorFile
	if err := json.Unmarshal(data, &vector); err != nil {
		t.Fatalf("decoding the known-answer vector: %v", err)
	}
	if vector.Version != Version {
		t.Fatalf("the vector is version %d, the package produces %d", vector.Version, Version)
	}
	return vector
}

// TestKnownAnswerVector seals the published plaintext with the fixed IV and
// requires the exact published envelope: a change to the format, the AAD or the
// encoding breaks this test instead of silently producing envelopes the page
// could not read.
func TestKnownAnswerVector(t *testing.T) {
	vector := loadVector(t)

	key, err := ParseKey(vector.Key)
	if err != nil {
		t.Fatalf("parsing the vector key: %v", err)
	}
	iv, err := base64.RawURLEncoding.DecodeString(vector.IV)
	if err != nil {
		t.Fatalf("decoding the vector iv: %v", err)
	}

	envelope, err := sealWithIV(key, vector.Board, []byte(vector.Plaintext), iv)
	if err != nil {
		t.Fatalf("sealing the vector: %v", err)
	}
	if envelope != vector.Envelope {
		t.Fatalf("the sealed vector differs from the published one:\n got: %s\nwant: %s", envelope, vector.Envelope)
	}

	plaintext, err := Open(key, vector.Board, vector.Envelope)
	if err != nil {
		t.Fatalf("opening the published envelope: %v", err)
	}
	if string(plaintext) != vector.Plaintext {
		t.Fatalf("the published envelope decrypts to %q, want %q", plaintext, vector.Plaintext)
	}
}

// TestVectorKeyIsPublished keeps a real key out of the file: the vector is served
// publicly under /ui/assets, so its key must stay the fixed test constant.
func TestVectorKeyIsPublished(t *testing.T) {
	vector := loadVector(t)

	key, err := ParseKey(vector.Key)
	if err != nil {
		t.Fatalf("parsing the vector key: %v", err)
	}

	expected := make([]byte, KeySize)
	for i := range expected {
		expected[i] = byte(i)
	}
	if string(key[:]) != string(expected) {
		t.Fatal("the vector key is not the published test constant: a real key must never live in ui/assets")
	}
}
