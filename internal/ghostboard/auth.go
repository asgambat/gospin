package ghostboard

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// SessionCookie carries the signed session token to the browser. The PIN
	// itself is never stored in a cookie.
	SessionCookie = "gb_pin_session"

	// MinPINLength and MaxPINLength bound the accepted PIN: four digits are
	// the shortest form, a longer passphrase is allowed and hashed the same
	// way, so choosing a stronger secret costs nothing.
	MinPINLength = 4
	MaxPINLength = 64

	// MinSecretLength is the shortest accepted secret. It keys both the PIN
	// digest and the session tokens, so it is the only thing between a copied
	// configuration file and the PIN.
	MinSecretLength = 16

	// tokenVersion tags the session token format, so a future change rejects
	// the tokens of the previous format instead of misreading them.
	tokenVersion = "v1"

	// pinDigestPrefix and sessionPrefix keep the two uses of the secret apart:
	// a session token can never be a valid PIN digest and vice versa.
	pinDigestPrefix = "gospin-ghostboard-pin:"
	sessionPrefix   = "gospin-ghostboard-session:"

	// sessionParts is the number of dot-separated fields of a session token.
	sessionParts = 3

	// lockoutCap bounds the backoff, so a client that keeps failing is slowed
	// down without being locked out forever.
	lockoutCap = time.Hour

	// limiterSweepThreshold is the number of tracked client addresses above
	// which stale entries are dropped, so attempts from many addresses cannot
	// grow the limiter without bound.
	limiterSweepThreshold = 1024
)

// Errors returned by the PIN gate.
var (
	// ErrPINRequired marks a request for a protected board without a valid
	// session.
	ErrPINRequired = errors.New("pin required")
	// ErrInvalidPIN marks a wrong PIN.
	ErrInvalidPIN = errors.New("invalid pin")
	// ErrPINNotConfigured marks an unlock attempt when no PIN is set up.
	ErrPINNotConfigured = errors.New("ghostboard pin is not configured")
	// ErrPINLength marks a PIN that cannot be accepted.
	ErrPINLength = fmt.Errorf("pin must be %d-%d characters", MinPINLength, MaxPINLength)
)

// AttemptsError reports a client that must wait before another PIN attempt.
// RetryAfter is the remaining lockout.
type AttemptsError struct {
	RetryAfter time.Duration
}

// Error renders the remaining lockout.
func (e *AttemptsError) Error() string {
	return fmt.Sprintf("too many pin attempts: retry in %s", e.RetryAfter.Round(time.Second))
}

// ValidatePIN reports whether pin can protect a board. A PIN is 4-64 printable
// characters, so the four digits of a numeric PIN and a longer passphrase are
// both accepted.
func ValidatePIN(pin string) error {
	if !utf8.ValidString(pin) {
		return ErrPINLength
	}
	length := utf8.RuneCountInString(pin)
	if length < MinPINLength || length > MaxPINLength {
		return ErrPINLength
	}
	for _, r := range pin {
		if unicode.IsControl(r) {
			return ErrPINLength
		}
	}
	return nil
}

// ValidatePINSecret reports whether secret can key the PIN digest and the
// session tokens.
func ValidatePINSecret(secret string) error {
	if len(secret) < MinSecretLength {
		return fmt.Errorf("ghostboard pin_secret must be at least %d characters", MinSecretLength)
	}
	return nil
}

// PINDigest returns the hex HMAC-SHA256 of a PIN. The server stores this digest
// in ghostboard.pin_hmac, so the configuration file never carries the PIN and a
// copy of the file cannot be brute-forced without the secret. The CLI uses the
// same function to generate the value.
func PINDigest(secret, pin string) (string, error) {
	if err := ValidatePIN(pin); err != nil {
		return "", err
	}
	if err := ValidatePINSecret(secret); err != nil {
		return "", err
	}
	return signHex(secret, pinDigestPrefix+pin), nil
}

// AuthConfig holds what the PIN gate needs. All fields are required when a PIN
// is configured.
type AuthConfig struct {
	// PINDigest is the hex HMAC of the PIN, as printed by PINDigest.
	PINDigest string
	// Secret keys the PIN digest and the session tokens.
	Secret string
	// PrivateBoards are the board names whose reads and writes need a session.
	PrivateBoards []string
	// SessionTTL is how long an unlock stays valid.
	SessionTTL time.Duration
	// MaxAttempts is the number of failures after which a client waits.
	MaxAttempts int
	// Lockout is the first wait; it doubles on every further MaxAttempts
	// failures, up to lockoutCap.
	Lockout time.Duration
}

// Authenticator verifies the shared PIN, issues session tokens and throttles
// failed attempts. A single PIN protects every board listed in PrivateBoards.
type Authenticator struct {
	private map[string]struct{}
	limiter *pinLimiter
	now     func() time.Time
	digest  []byte
	secret  []byte
	ttl     time.Duration
}

// NewAuthenticator validates the PIN settings and builds the gate.
func NewAuthenticator(cfg AuthConfig) (*Authenticator, error) {
	if cfg.SessionTTL <= 0 {
		return nil, errors.New("ghostboard session_ttl_secs must be positive")
	}
	if cfg.MaxAttempts <= 0 {
		return nil, errors.New("ghostboard pin_max_attempts must be positive")
	}
	if cfg.Lockout <= 0 {
		return nil, errors.New("ghostboard pin_lockout_secs must be positive")
	}
	if err := ValidatePINSecret(cfg.Secret); err != nil {
		return nil, err
	}

	digest, err := hex.DecodeString(strings.TrimSpace(cfg.PINDigest))
	if err != nil || len(digest) != sha256.Size {
		return nil, fmt.Errorf("ghostboard pin_hmac must be the hex HMAC-SHA256 of the PIN (%d hex characters)", sha256.Size*2)
	}

	private := make(map[string]struct{}, len(cfg.PrivateBoards))
	for _, name := range cfg.PrivateBoards {
		name = strings.TrimSpace(name)
		if err := ValidateBoardName(name); err != nil {
			return nil, fmt.Errorf("ghostboard private_boards contains an invalid board name %q: %w", name, err)
		}
		private[name] = struct{}{}
	}

	return &Authenticator{
		digest:  digest,
		secret:  []byte(cfg.Secret),
		private: private,
		ttl:     cfg.SessionTTL,
		limiter: &pinLimiter{
			entries: make(map[string]*pinAttempts),
			max:     cfg.MaxAttempts,
			lockout: cfg.Lockout,
		},
		now: time.Now,
	}, nil
}

// IsPrivate reports whether board needs a session for reading and writing.
func (a *Authenticator) IsPrivate(board string) bool {
	if a == nil {
		return false
	}
	_, ok := a.private[board]
	return ok
}

// SessionTTL returns how long an issued session stays valid.
func (a *Authenticator) SessionTTL() time.Duration {
	if a == nil {
		return 0
	}
	return a.ttl
}

// VerifyPIN reports whether pin matches the configured digest. The comparison
// runs in constant time, so a wrong PIN never leaks how much of it was right.
func (a *Authenticator) VerifyPIN(pin string) bool {
	if a == nil || ValidatePIN(pin) != nil {
		return false
	}
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(pinDigestPrefix + pin))
	return hmac.Equal(mac.Sum(nil), a.digest)
}

// UnlockResult is a successful unlock: the token to send back and its expiry.
type UnlockResult struct {
	Expires time.Time
	Token   string
}

// Unlock verifies a PIN for a client address and returns a session token. The
// attempts of the address are throttled: a client that keeps failing gets an
// *AttemptsError carrying how long it must wait.
func (a *Authenticator) Unlock(ip, pin string) (UnlockResult, error) {
	if a == nil {
		return UnlockResult{}, ErrPINNotConfigured
	}

	now := a.now()
	if wait := a.limiter.retryAfter(ip, now); wait > 0 {
		return UnlockResult{}, &AttemptsError{RetryAfter: wait}
	}
	// A malformed PIN is a usage error, not a guess: it is rejected without
	// counting against the client, so an empty form field never locks anyone
	// out. A brute-force attempt always has a valid format anyway.
	if err := ValidatePIN(pin); err != nil {
		return UnlockResult{}, err
	}
	if !a.VerifyPIN(pin) {
		a.limiter.fail(ip, now)
		return UnlockResult{}, ErrInvalidPIN
	}

	a.limiter.reset(ip)
	token, expires := a.IssueToken()
	return UnlockResult{Token: token, Expires: expires}, nil
}

// IssueToken returns a signed session token and its expiry. The token is
// stateless: it carries its expiry and is verified with the secret, so a server
// restart keeps the sessions it issued alive.
func (a *Authenticator) IssueToken() (string, time.Time) {
	expires := a.now().Add(a.ttl)
	payload := tokenVersion + "." + strconv.FormatInt(expires.Unix(), 10)
	return payload + "." + signHex(string(a.secret), sessionPrefix+payload), expires
}

// ValidToken reports whether token was issued by this server and has not
// expired.
func (a *Authenticator) ValidToken(token string) bool {
	if a == nil || token == "" {
		return false
	}

	parts := strings.Split(token, ".")
	if len(parts) != sessionParts || parts[0] != tokenVersion {
		return false
	}

	expiry, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return false
	}

	payload := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(parts[2]), []byte(signHex(string(a.secret), sessionPrefix+payload))) {
		return false
	}

	return a.now().Unix() < expiry
}

// signHex returns the hex HMAC-SHA256 of message under secret.
func signHex(secret, message string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

// pinLimiter throttles failed PIN attempts per client address. It is keyed by
// the address the TCP connection came from, never by a client-supplied header,
// so a spoofed X-Forwarded-For cannot bypass it.
type pinLimiter struct {
	entries map[string]*pinAttempts
	max     int
	lockout time.Duration
	mu      sync.Mutex
}

// pinAttempts is the state of one client address.
type pinAttempts struct {
	until    time.Time
	lastSeen time.Time
	failures int
}

// retryAfter returns how long the client must still wait, zero when it may try
// now.
func (l *pinLimiter) retryAfter(key string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.entries[key]
	if !ok || !entry.until.After(now) {
		return 0
	}
	return entry.until.Sub(now)
}

// fail records a failed attempt and starts (or extends) the lockout once the
// client reached the configured limit.
func (l *pinLimiter) fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.entries[key]
	if !ok {
		entry = &pinAttempts{}
		l.entries[key] = entry
	}
	entry.failures++
	entry.lastSeen = now

	if entry.failures >= l.max {
		entry.until = now.Add(l.backoff(entry.failures))
	}

	l.sweepLocked(now)
}

// reset forgets a client that authenticated, so a correct PIN clears the
// backoff built up by earlier typos.
func (l *pinLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.entries, key)
}

// backoff returns the wait for a client with the given number of failures: the
// base lockout after the first MaxAttempts failures, doubled every further
// batch, never beyond lockoutCap.
func (l *pinLimiter) backoff(failures int) time.Duration {
	delay := l.lockout
	for batch := failures / l.max; batch > 1 && delay < lockoutCap; batch-- {
		delay *= 2
	}
	if delay > lockoutCap {
		return lockoutCap
	}
	return delay
}

// sweepLocked drops the entries that no longer matter once the map grew past
// the threshold. It runs under the limiter mutex.
func (l *pinLimiter) sweepLocked(now time.Time) {
	if len(l.entries) <= limiterSweepThreshold {
		return
	}
	for key, entry := range l.entries {
		if entry.until.After(now) {
			continue
		}
		if now.Sub(entry.lastSeen) > lockoutCap {
			delete(l.entries, key)
		}
	}
}
