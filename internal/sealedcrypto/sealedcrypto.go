// Package sealedcrypto implements the envelope of a sealed ghostboard: the
// client encrypts the text with AES-256-GCM before writing and decrypts it after
// reading, so the server only ever stores ciphertext it cannot read.
//
// The key is 256 random bits carried by the client — in the URL fragment for the
// page, in --key, --key-file or GO_SPIN_BOARD_KEY for gb-cli — and never sent to
// the server. Every envelope carries its own 96-bit IV, and the board name is the
// additional authenticated data, so an envelope copied under another board name
// fails to decrypt instead of decoding into the wrong place.
//
// The package is used by gb-cli. The browser page implements the same envelope
// with WebCrypto (ui/assets/sealed.js) and both are pinned by one known-answer
// vector, so the two encoders cannot drift apart silently.
package sealedcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	// KeySize is the size of a sealed board key in bytes.
	KeySize = 32

	// IVSize is the size of the nonce of an envelope in bytes.
	IVSize = 12

	// TagSize is the AES-GCM authentication tag appended to the ciphertext.
	TagSize = 16

	// Version is the envelope version this package produces. A future cipher
	// gets a new value instead of a silent reinterpretation of this one.
	Version = 1

	// aadPrefix is prepended to the board name to build the additional
	// authenticated data of an envelope.
	aadPrefix = "gospin-sealed-v1|"

	// envelopeLenBase is the exact number of envelope characters that do not
	// depend on the plaintext: `{"v":1,"iv":"` + the IV + `","ct":"` + `"}`.
	envelopeLenBase = 13 + 16 + 8 + 2
)

// Errors returned by the package. ErrDecrypt deliberately covers a wrong key, a
// wrong board name and a tampered ciphertext: a caller must not be able to tell
// them apart, and the message never carries secret material.
var (
	ErrInvalidKey         = errors.New("invalid sealed board key")
	ErrInvalidEnvelope    = errors.New("invalid sealed envelope")
	ErrUnsupportedVersion = errors.New("unsupported sealed envelope version")
	ErrDecrypt            = errors.New("cannot decrypt the sealed envelope")
)

// Key is a sealed board key.
type Key [KeySize]byte

// Envelope is the JSON shape stored for a sealed board. The struct layout keeps
// the strings first so it has no padding; MarshalJSON spells out the wire order
// the known-answer vector pins.
type Envelope struct {
	IV      string `json:"iv"`
	CT      string `json:"ct"`
	Version int    `json:"v"`
}

// MarshalJSON writes the envelope as {"v":1,"iv":"…","ct":"…"}: the field
// order of the wire form is not the field order of the struct, and the vector
// pins the former. The two strings are encoded by encoding/json so the output is
// valid JSON whatever they hold.
func (e Envelope) MarshalJSON() ([]byte, error) {
	iv, err := json.Marshal(e.IV)
	if err != nil {
		return nil, err
	}
	ct, err := json.Marshal(e.CT)
	if err != nil {
		return nil, err
	}

	return fmt.Appendf(nil, `{"v":%d,"iv":%s,"ct":%s}`, e.Version, iv, ct), nil
}

// NewKey mints a random key.
func NewKey() (Key, error) {
	var key Key
	if _, err := rand.Read(key[:]); err != nil {
		return Key{}, fmt.Errorf("generating a sealed board key: %w", err)
	}
	return key, nil
}

// String returns the key in the base64url form used in a link fragment and by
// gb-cli, 43 characters without padding.
func (k Key) String() string {
	return base64.RawURLEncoding.EncodeToString(k[:])
}

// IsZero reports whether the key was never set, which is how a missing key is
// detected before a request is attempted.
func (k Key) IsZero() bool {
	return k == Key{}
}

// ParseKey reads a key from the accepted textual forms: base64url (with or
// without padding), lowercase or uppercase hex, and a whole sealed board link
// whose fragment carries `k=<key>`. Surrounding whitespace is dropped so a key
// copied from a terminal or a chat window still works.
func ParseKey(text string) (Key, error) {
	trimmed := strings.TrimSpace(text)
	if fragment, ok := fragmentOf(trimmed); ok {
		trimmed = fragment
	}
	if trimmed == "" {
		return Key{}, ErrInvalidKey
	}

	if len(trimmed) == hex.EncodedLen(KeySize) {
		var key Key
		if decoded, err := hex.DecodeString(trimmed); err == nil {
			copy(key[:], decoded)
			return key, nil
		}
	}

	for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding} {
		decoded, err := encoding.DecodeString(trimmed)
		if err != nil {
			continue
		}
		if len(decoded) != KeySize {
			return Key{}, fmt.Errorf("%w: the key must be %d bytes, got %d", ErrInvalidKey, KeySize, len(decoded))
		}
		var key Key
		copy(key[:], decoded)
		return key, nil
	}

	return Key{}, fmt.Errorf("%w: expected base64url or hex", ErrInvalidKey)
}

// fragmentOf extracts the key carried after the `#` of a link, including the
// `k=` prefix the documentation uses. A string without a fragment is returned
// unchanged by the caller.
func fragmentOf(text string) (string, bool) {
	index := strings.Index(text, "#")
	if index < 0 {
		return "", false
	}
	fragment := text[index+1:]
	if rest, ok := strings.CutPrefix(fragment, "k="); ok {
		fragment = rest
	}
	return strings.TrimSpace(fragment), true
}

// ParseEnvelope validates the shape of an envelope — the version, the IV length
// and the base64 of both fields — without decrypting it. It is what the store
// calls before accepting a sealed write, so the shape is checked in one place.
func ParseEnvelope(envelope string) (Envelope, error) {
	var parsed Envelope
	if err := json.Unmarshal([]byte(envelope), &parsed); err != nil {
		return Envelope{}, fmt.Errorf("%w: %v", ErrInvalidEnvelope, err)
	}
	if parsed.Version != Version {
		return Envelope{}, fmt.Errorf("%w: %d", ErrUnsupportedVersion, parsed.Version)
	}

	iv, err := base64.RawURLEncoding.DecodeString(parsed.IV)
	if err != nil || len(iv) != IVSize {
		return Envelope{}, fmt.Errorf("%w: the iv must be %d base64url bytes", ErrInvalidEnvelope, IVSize)
	}

	ct, err := base64.RawURLEncoding.DecodeString(parsed.CT)
	if err != nil || len(ct) < TagSize {
		return Envelope{}, fmt.Errorf("%w: the ciphertext must carry a %d byte tag", ErrInvalidEnvelope, TagSize)
	}

	return parsed, nil
}

// Seal encrypts plaintext for the given board and returns the JSON envelope.
func Seal(key Key, board string, plaintext []byte) (string, error) {
	iv := make([]byte, IVSize)
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("generating the envelope iv: %w", err)
	}
	return sealWithIV(key, board, plaintext, iv)
}

// sealWithIV encrypts with a caller-supplied IV. It exists for the known-answer
// vector, which has to reproduce an envelope byte for byte; production code goes
// through Seal and its random IV.
func sealWithIV(key Key, board string, plaintext, iv []byte) (string, error) {
	if len(iv) != IVSize {
		return "", fmt.Errorf("the envelope iv must be %d bytes, got %d", IVSize, len(iv))
	}

	gcm, err := gcmFor(key)
	if err != nil {
		return "", err
	}

	sealed := gcm.Seal(nil, iv, plaintext, aad(board))
	envelope, err := json.Marshal(Envelope{
		Version: Version,
		IV:      base64.RawURLEncoding.EncodeToString(iv),
		CT:      base64.RawURLEncoding.EncodeToString(sealed),
	})
	if err != nil {
		return "", fmt.Errorf("encoding the sealed envelope: %w", err)
	}
	return string(envelope), nil
}

// Open decrypts an envelope of the given board.
func Open(key Key, board, envelope string) ([]byte, error) {
	parsed, err := ParseEnvelope(envelope)
	if err != nil {
		return nil, err
	}

	iv, err := base64.RawURLEncoding.DecodeString(parsed.IV)
	if err != nil {
		return nil, fmt.Errorf("%w: the iv is not base64url", ErrInvalidEnvelope)
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(parsed.CT)
	if err != nil {
		return nil, fmt.Errorf("%w: the ciphertext is not base64url", ErrInvalidEnvelope)
	}

	gcm, err := gcmFor(key)
	if err != nil {
		return nil, err
	}

	plaintext, err := gcm.Open(nil, iv, ciphertext, aad(board))
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}

// EnvelopeLen returns the exact size in bytes of the envelope that sealing
// plaintextLen bytes produces. max_text_bytes bounds the envelope, and base64
// grows the ciphertext by about a third, so the limit of the typed text has to
// be computed from it instead of assumed.
func EnvelopeLen(plaintextLen int) int {
	if plaintextLen < 0 {
		plaintextLen = 0
	}
	return envelopeLenBase + rawBase64Len(plaintextLen+TagSize)
}

// MaxPlaintextBytes returns the largest plaintext whose envelope fits in
// envelopeLimit bytes, or a negative value when not even an empty text fits.
//
// EnvelopeLen bounds the base64 of the ciphertext, and ceil(4m/3) <= C is
// equivalent to m <= floor(3C/4), so the answer is exact rather than a search.
func MaxPlaintextBytes(envelopeLimit int) int {
	available := envelopeLimit - envelopeLenBase
	if available < rawBase64Len(TagSize) {
		return -1
	}
	return (3*available)/4 - TagSize
}

// rawBase64Len returns the length of the unpadded base64 encoding of n bytes.
func rawBase64Len(n int) int {
	return (4*n + 2) / 3
}

// gcmFor builds the AEAD of a key.
func gcmFor(key Key) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	return gcm, nil
}

// aad builds the additional authenticated data of a board, which binds an
// envelope to the board name it was written to.
func aad(board string) []byte {
	return []byte(aadPrefix + board)
}
