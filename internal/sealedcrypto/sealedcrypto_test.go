package sealedcrypto

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// testKey returns a deterministic key so a failure is reproducible.
func testKey(t *testing.T) Key {
	t.Helper()
	key, err := ParseKey(strings.Repeat("0123456789abcdef", 4))
	if err != nil {
		t.Fatalf("building the test key: %v", err)
	}
	return key
}

func TestSealRoundTrip(t *testing.T) {
	key := testKey(t)

	texts := map[string]string{
		"empty":        "",
		"ascii":        "hello board",
		"multibyte":    "caffè e tè ☕ — nota",
		"large":        strings.Repeat("x", 4096),
		"with newline": "line one\nline two\n",
	}

	for name, plaintext := range texts {
		t.Run(name, func(t *testing.T) {
			envelope, err := Seal(key, "nota", []byte(plaintext))
			if err != nil {
				t.Fatalf("Seal returned an error: %v", err)
			}
			if _, err := ParseEnvelope(envelope); err != nil {
				t.Fatalf("the produced envelope does not parse: %v", err)
			}

			recovered, err := Open(key, "nota", envelope)
			if err != nil {
				t.Fatalf("Open returned an error: %v", err)
			}
			if string(recovered) != plaintext {
				t.Fatalf("Open returned %q, want %q", recovered, plaintext)
			}
		})
	}
}

func TestSealUsesFreshIV(t *testing.T) {
	key := testKey(t)

	first, err := Seal(key, "nota", []byte("same text"))
	if err != nil {
		t.Fatalf("first Seal: %v", err)
	}
	second, err := Seal(key, "nota", []byte("same text"))
	if err != nil {
		t.Fatalf("second Seal: %v", err)
	}
	if first == second {
		t.Fatal("two seals of the same text produced the same envelope: the iv is not fresh")
	}
}

func TestOpenRejectsWrongInput(t *testing.T) {
	key := testKey(t)
	otherKey, err := NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}

	envelope, err := Seal(key, "nota", []byte("segreto"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	t.Run("tampered ciphertext", func(t *testing.T) {
		parsed, err := ParseEnvelope(envelope)
		if err != nil {
			t.Fatalf("ParseEnvelope: %v", err)
		}
		raw, err := base64.RawURLEncoding.DecodeString(parsed.CT)
		if err != nil {
			t.Fatalf("decoding the ciphertext: %v", err)
		}
		raw[0] ^= 0xff
		tampered, err := json.Marshal(Envelope{
			Version: Version,
			IV:      parsed.IV,
			CT:      base64.RawURLEncoding.EncodeToString(raw),
		})
		if err != nil {
			t.Fatalf("encoding the tampered envelope: %v", err)
		}
		if _, err := Open(key, "nota", string(tampered)); !errors.Is(err, ErrDecrypt) {
			t.Fatalf("Open returned %v, want ErrDecrypt", err)
		}
	})

	t.Run("wrong key", func(t *testing.T) {
		if _, err := Open(otherKey, "nota", envelope); !errors.Is(err, ErrDecrypt) {
			t.Fatalf("Open returned %v, want ErrDecrypt", err)
		}
	})

	t.Run("wrong board in the aad", func(t *testing.T) {
		if _, err := Open(key, "altra", envelope); !errors.Is(err, ErrDecrypt) {
			t.Fatalf("Open returned %v, want ErrDecrypt", err)
		}
	})
}

func TestParseEnvelopeRejectsMalformed(t *testing.T) {
	cases := map[string]struct {
		want     error
		envelope string
	}{
		"not json":            {envelope: "not an envelope", want: ErrInvalidEnvelope},
		"empty":               {envelope: "", want: ErrInvalidEnvelope},
		"future version":      {envelope: `{"v":2,"iv":"ICEiIyQlJicoKSor","ct":"plLDUB3tc21xXCC8rm-a2bYmlLzt9Q6eH9YDZCz4fn0d68Vo719M-nCSCUmegN1BRDlfqpqcSBx7T2w"}`, want: ErrUnsupportedVersion},
		"missing version":     {envelope: `{"iv":"ICEiIyQlJicoKSor","ct":"plLDUB3tc21xXCC8rm-a2bYmlLzt9Q6eH9YDZCz4fn0d68Vo719M-nCSCUmegN1BRDlfqpqcSBx7T2w"}`, want: ErrUnsupportedVersion},
		"short iv":            {envelope: `{"v":1,"iv":"ICEiIyQlJico","ct":"plLDUB3tc21xXCC8rm-a2bYmlLzt9Q6eH9YDZCz4fn0d68Vo719M-nCSCUmegN1BRDlfqpqcSBx7T2w"}`, want: ErrInvalidEnvelope},
		"iv is not base64url": {envelope: `{"v":1,"iv":"ICEiIyQlJicoKSor!!","ct":"plLDUB3tc21xXCC8rm-a2bYmlLzt9Q6eH9YDZCz4fn0d68Vo719M-nCSCUmegN1BRDlfqpqcSBx7T2w"}`, want: ErrInvalidEnvelope},
		"ciphertext too short": {
			envelope: `{"v":1,"iv":"ICEiIyQlJicoKSor","ct":"AAAA"}`,
			want:     ErrInvalidEnvelope,
		},
		"ciphertext not base64url": {
			envelope: `{"v":1,"iv":"ICEiIyQlJicoKSor","ct":"////"}`, want: ErrInvalidEnvelope,
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseEnvelope(testCase.envelope); !errors.Is(err, testCase.want) {
				t.Fatalf("ParseEnvelope returned %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestParseKey(t *testing.T) {
	want := testKey(t)
	keyBytes := strings.Repeat("0123456789abcdef", 4)

	link := "https://spin.local/ui/sealed/nota#k=" + base64.RawURLEncoding.EncodeToString(want[:])

	valid := map[string]string{
		"base64url":       want.String(),
		"hex":             keyBytes,
		"upper hex":       strings.ToUpper(keyBytes),
		"padded base64":   base64.URLEncoding.EncodeToString(want[:]),
		"surrounding gap": "  " + want.String() + "\n",
		"link with k=":    link,
		"fragment only":   "#" + want.String(),
		"relative link":   "/ui/sealed/nota#k=" + base64.RawURLEncoding.EncodeToString(want[:]),
		"hex through hex.DecodeString": func() string {
			return hex.EncodeToString(want[:])
		}(),
	}

	for name, text := range valid {
		t.Run(name, func(t *testing.T) {
			key, err := ParseKey(text)
			if err != nil {
				t.Fatalf("ParseKey(%q) returned an error: %v", text, err)
			}
			if key != want {
				t.Fatalf("ParseKey(%q) returned a different key", text)
			}
		})
	}

	invalid := map[string]string{
		"empty":          "",
		"blank":          "   ",
		"empty fragment": "#",
		"too short":      base64.RawURLEncoding.EncodeToString(want[:31]),
		"too long":       base64.RawURLEncoding.EncodeToString(append(append([]byte(nil), want[:]...), 0x00)),
		"bad alphabet":   "not-a-key-with-invalid-characters-here!!",
	}

	for name, text := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseKey(text); !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("ParseKey(%q) returned %v, want ErrInvalidKey", text, err)
			}
		})
	}
}

func TestKeyStringRoundTrip(t *testing.T) {
	key, err := NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	if key.IsZero() {
		t.Fatal("NewKey returned the zero key")
	}

	text := key.String()
	if len(text) != 43 {
		t.Fatalf("the encoded key is %d characters, want 43", len(text))
	}

	parsed, err := ParseKey(text)
	if err != nil {
		t.Fatalf("ParseKey of the encoded key: %v", err)
	}
	if parsed != key {
		t.Fatal("the key changed across String and ParseKey")
	}

	var zero Key
	if !zero.IsZero() {
		t.Fatal("the zero key is not reported as zero")
	}
}

func TestEnvelopeLenMatchesSeal(t *testing.T) {
	key := testKey(t)

	for _, size := range []int{0, 1, 2, 3, 4, 16, 17, 100, 1000, 33333} {
		envelope, err := Seal(key, "nota", []byte(strings.Repeat("a", size)))
		if err != nil {
			t.Fatalf("Seal of %d bytes: %v", size, err)
		}
		if got, want := len(envelope), EnvelopeLen(size); got != want {
			t.Fatalf("EnvelopeLen(%d) = %d, but the sealed envelope is %d bytes", size, want, got)
		}
	}
}

func TestMaxPlaintextBytesInvertsEnvelopeLen(t *testing.T) {
	if got := MaxPlaintextBytes(EnvelopeLen(0) - 1); got >= 0 {
		t.Fatalf("MaxPlaintextBytes below the size of an empty envelope = %d, want a negative value", got)
	}
	if got := MaxPlaintextBytes(EnvelopeLen(0)); got != 0 {
		t.Fatalf("MaxPlaintextBytes(EnvelopeLen(0)) = %d, want 0", got)
	}

	for limit := EnvelopeLen(0); limit < 400; limit++ {
		fits := MaxPlaintextBytes(limit)
		if fits < 0 {
			t.Fatalf("MaxPlaintextBytes(%d) = %d, want a non-negative value", limit, fits)
		}
		if got := EnvelopeLen(fits); got > limit {
			t.Fatalf("MaxPlaintextBytes(%d) = %d, whose envelope is %d bytes", limit, fits, got)
		}
		if got := EnvelopeLen(fits + 1); got <= limit {
			t.Fatalf("MaxPlaintextBytes(%d) = %d, but %d bytes also fit (%d)", limit, fits, fits+1, got)
		}
	}
}
