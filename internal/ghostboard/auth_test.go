package ghostboard

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const testSecret = "0123456789abcdef-secret"

func newTestAuthenticator(t *testing.T, mutate func(*AuthConfig)) *Authenticator {
	t.Helper()

	digest, err := PINDigest(testSecret, "1234")
	if err != nil {
		t.Fatalf("PINDigest: %v", err)
	}

	cfg := AuthConfig{
		PINDigest:     digest,
		Secret:        testSecret,
		PrivateBoards: []string{"lavoro"},
		SessionTTL:    8 * time.Hour,
		MaxAttempts:   3,
		Lockout:       time.Minute,
	}
	if mutate != nil {
		mutate(&cfg)
	}

	auth, err := NewAuthenticator(cfg)
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	return auth
}

func TestValidatePIN(t *testing.T) {
	tests := []struct {
		name string
		pin  string
		ok   bool
	}{
		{name: "four digits", pin: "1234", ok: true},
		{name: "four letters", pin: "abcd", ok: true},
		{name: "passphrase", pin: "correct horse battery", ok: true},
		{name: "maximum length", pin: strings.Repeat("a", MaxPINLength), ok: true},
		{name: "too short", pin: "123", ok: false},
		{name: "empty", pin: "", ok: false},
		{name: "too long", pin: strings.Repeat("a", MaxPINLength+1), ok: false},
		{name: "newline", pin: "12\n4", ok: false},
		{name: "invalid utf-8", pin: "12\xff4", ok: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidatePIN(test.pin)
			if test.ok && err != nil {
				t.Errorf("ValidatePIN(%q) = %v, want nil", test.pin, err)
			}
			if !test.ok && !errors.Is(err, ErrPINLength) {
				t.Errorf("ValidatePIN(%q) = %v, want ErrPINLength", test.pin, err)
			}
		})
	}
}

func TestPINDigest(t *testing.T) {
	first, err := PINDigest(testSecret, "1234")
	if err != nil {
		t.Fatalf("PINDigest: %v", err)
	}
	second, err := PINDigest(testSecret, "1234")
	if err != nil {
		t.Fatalf("PINDigest: %v", err)
	}
	if first != second {
		t.Errorf("PINDigest is not deterministic: %q != %q", first, second)
	}
	if len(first) != 64 {
		t.Errorf("PINDigest length = %d, want 64 hex characters", len(first))
	}

	other, err := PINDigest("another-secret-value", "1234")
	if err != nil {
		t.Fatalf("PINDigest with another secret: %v", err)
	}
	if other == first {
		t.Error("PINDigest ignores the secret: a copied configuration could be brute-forced")
	}

	if _, err := PINDigest("short", "1234"); err == nil {
		t.Error("PINDigest accepted a short secret")
	}
	if _, err := PINDigest(testSecret, "12"); !errors.Is(err, ErrPINLength) {
		t.Errorf("PINDigest with a short pin = %v, want ErrPINLength", err)
	}
}

func TestNewAuthenticator_Validation(t *testing.T) {
	tests := []struct {
		mutate func(*AuthConfig)
		name   string
	}{
		{name: "bad hex", mutate: func(c *AuthConfig) { c.PINDigest = "zz" }},
		{name: "wrong digest length", mutate: func(c *AuthConfig) { c.PINDigest = "abcd" }},
		{name: "missing secret", mutate: func(c *AuthConfig) { c.Secret = "" }},
		{name: "short secret", mutate: func(c *AuthConfig) { c.Secret = "short" }},
		{name: "zero session ttl", mutate: func(c *AuthConfig) { c.SessionTTL = 0 }},
		{name: "zero attempts", mutate: func(c *AuthConfig) { c.MaxAttempts = 0 }},
		{name: "zero lockout", mutate: func(c *AuthConfig) { c.Lockout = 0 }},
		{name: "invalid board name", mutate: func(c *AuthConfig) { c.PrivateBoards = []string{"not a board"} }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			digest, err := PINDigest(testSecret, "1234")
			if err != nil {
				t.Fatalf("PINDigest: %v", err)
			}
			cfg := AuthConfig{
				PINDigest:     digest,
				Secret:        testSecret,
				PrivateBoards: []string{"lavoro"},
				SessionTTL:    time.Hour,
				MaxAttempts:   3,
				Lockout:       time.Minute,
			}
			test.mutate(&cfg)
			if _, err := NewAuthenticator(cfg); err == nil {
				t.Error("NewAuthenticator accepted an invalid configuration")
			}
		})
	}
}

func TestAuthenticator_IsPrivate(t *testing.T) {
	auth := newTestAuthenticator(t, nil)

	if !auth.IsPrivate("lavoro") {
		t.Error("IsPrivate(lavoro) = false, want true")
	}
	if auth.IsPrivate("note") {
		t.Error("IsPrivate(note) = true, want false")
	}
	var nilAuth *Authenticator
	if nilAuth.IsPrivate("lavoro") {
		t.Error("a nil authenticator must not protect a board")
	}
}

func TestAuthenticator_VerifyPIN(t *testing.T) {
	auth := newTestAuthenticator(t, nil)

	if !auth.VerifyPIN("1234") {
		t.Error("VerifyPIN rejected the configured PIN")
	}
	for _, wrong := range []string{"1235", "12345", "", "pass"} {
		if auth.VerifyPIN(wrong) {
			t.Errorf("VerifyPIN accepted %q", wrong)
		}
	}
}

func TestAuthenticator_Unlock(t *testing.T) {
	auth := newTestAuthenticator(t, nil)
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	auth.now = func() time.Time { return now }

	result, err := auth.Unlock("192.0.2.10", "1234")
	if err != nil {
		t.Fatalf("Unlock with the right pin: %v", err)
	}
	if result.Token == "" {
		t.Fatal("Unlock returned an empty token")
	}
	if !result.Expires.Equal(now.Add(8 * time.Hour)) {
		t.Errorf("expiry = %v, want %v", result.Expires, now.Add(8*time.Hour))
	}
	if !auth.ValidToken(result.Token) {
		t.Error("ValidToken rejected a token it just issued")
	}

	if _, err := auth.Unlock("192.0.2.10", "9999"); !errors.Is(err, ErrInvalidPIN) {
		t.Errorf("Unlock with a wrong pin = %v, want ErrInvalidPIN", err)
	}
}

func TestAuthenticator_UnlockRequiresConfiguredPIN(t *testing.T) {
	var auth *Authenticator
	if _, err := auth.Unlock("192.0.2.10", "1234"); !errors.Is(err, ErrPINNotConfigured) {
		t.Errorf("Unlock on a server without a pin = %v, want ErrPINNotConfigured", err)
	}
}

func TestAuthenticator_UnlockLocksOutAfterFailures(t *testing.T) {
	auth := newTestAuthenticator(t, func(c *AuthConfig) {
		c.MaxAttempts = 2
		c.Lockout = time.Minute
	})
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	auth.now = func() time.Time { return now }

	for i := 0; i < 2; i++ {
		if _, err := auth.Unlock("192.0.2.10", "0000"); !errors.Is(err, ErrInvalidPIN) {
			t.Fatalf("attempt %d = %v, want ErrInvalidPIN", i+1, err)
		}
	}

	_, err := auth.Unlock("192.0.2.10", "1234")
	var attemptsErr *AttemptsError
	if !errors.As(err, &attemptsErr) {
		t.Fatalf("third attempt = %v, want AttemptsError", err)
	}
	if attemptsErr.RetryAfter != time.Minute {
		t.Errorf("RetryAfter = %v, want 1m", attemptsErr.RetryAfter)
	}

	// Another address is unaffected: the limiter is per client.
	if _, err := auth.Unlock("198.51.100.7", "1234"); err != nil {
		t.Errorf("Unlock from another address = %v, want nil", err)
	}

	// After the lockout the right PIN is accepted again and clears the counter.
	now = now.Add(time.Minute)
	if _, err := auth.Unlock("192.0.2.10", "1234"); err != nil {
		t.Fatalf("Unlock after the lockout = %v, want nil", err)
	}
}

func TestAuthenticator_UnlockIgnoresMalformedPINForTheLimiter(t *testing.T) {
	auth := newTestAuthenticator(t, func(c *AuthConfig) { c.MaxAttempts = 1 })
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	auth.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if _, err := auth.Unlock("192.0.2.10", "12"); !errors.Is(err, ErrPINLength) {
			t.Fatalf("malformed pin %d = %v, want ErrPINLength", i+1, err)
		}
	}

	if _, err := auth.Unlock("192.0.2.10", "1234"); err != nil {
		t.Errorf("a malformed pin must not lock the client out: %v", err)
	}
}

func TestAuthenticator_UnlockBackoffDoublesWithFailures(t *testing.T) {
	auth := newTestAuthenticator(t, func(c *AuthConfig) {
		c.MaxAttempts = 1
		c.Lockout = time.Minute
	})
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	auth.now = func() time.Time { return now }

	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute}
	for i, expected := range want {
		if _, err := auth.Unlock("192.0.2.10", "0000"); !errors.Is(err, ErrInvalidPIN) {
			t.Fatalf("failure %d = %v, want ErrInvalidPIN", i+1, err)
		}
		_, err := auth.Unlock("192.0.2.10", "1234")
		var attemptsErr *AttemptsError
		if !errors.As(err, &attemptsErr) {
			t.Fatalf("attempt %d = %v, want AttemptsError", i+1, err)
		}
		if attemptsErr.RetryAfter != expected {
			t.Errorf("RetryAfter after %d failures = %v, want %v", i+1, attemptsErr.RetryAfter, expected)
		}
		now = now.Add(expected)
	}
}

func TestAuthenticator_TokenRejectsTampering(t *testing.T) {
	auth := newTestAuthenticator(t, nil)
	token, _ := auth.IssueToken()

	tests := []struct {
		name  string
		token string
	}{
		{name: "empty", token: ""},
		{name: "not a token", token: "nope"},
		{name: "missing signature", token: strings.Join(strings.Split(token, ".")[:2], ".")},
		{name: "unknown version", token: "v2" + strings.TrimPrefix(token, "v1")},
		{name: "flipped signature", token: token[:len(token)-1] + "0"},
		{name: "expiry swapped", token: strings.Replace(token, "."+strings.Split(token, ".")[1]+".", ".9999999999.", 1)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if auth.ValidToken(test.token) {
				t.Errorf("ValidToken accepted %q", test.token)
			}
		})
	}
}

func TestAuthenticator_TokenExpiry(t *testing.T) {
	auth := newTestAuthenticator(t, func(c *AuthConfig) { c.SessionTTL = time.Minute })
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	auth.now = func() time.Time { return now }

	token, expires := auth.IssueToken()
	if !auth.ValidToken(token) {
		t.Fatal("ValidToken rejected a fresh token")
	}

	now = expires.Add(time.Second)
	if auth.ValidToken(token) {
		t.Error("ValidToken accepted an expired token")
	}
}

func TestAuthenticator_TokenSurvivesRestart(t *testing.T) {
	first := newTestAuthenticator(t, nil)
	token, _ := first.IssueToken()

	restarted := newTestAuthenticator(t, nil)
	if !restarted.ValidToken(token) {
		t.Error("a token issued before the restart must stay valid: the token is stateless")
	}

	otherSecret := newTestAuthenticator(t, func(c *AuthConfig) {
		digest, err := PINDigest("another-secret-value", "1234")
		if err != nil {
			t.Fatalf("PINDigest: %v", err)
		}
		c.PINDigest = digest
		c.Secret = "another-secret-value"
	})
	if otherSecret.ValidToken(token) {
		t.Error("a token must not validate under another secret")
	}
}
