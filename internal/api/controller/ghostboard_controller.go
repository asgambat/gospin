package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/asgambat/gospin/internal/ghostboard"
	"github.com/asgambat/gospin/internal/logger"
	"github.com/asgambat/gospin/internal/sealedcrypto"
)

const (
	// ghostboardClientHeader carries the per-tab identifier used to suppress
	// the echo of a client's own writes in its event stream.
	ghostboardClientHeader = "X-Ghostboard-Client"
	ghostboardClientIDMax  = 64

	// ghostboardBodyOverhead leaves room for the envelope around the text (JSON
	// keys, form field name) when enforcing the configured board size on the
	// request body.
	ghostboardBodyOverhead = 1024

	// ghostboardJSONEscapeFactor bounds how much a single text byte can grow when
	// encoded inside a JSON string: encoding/json writes control characters and
	// HTML-sensitive bytes as \u00XX, i.e. six bytes per input byte. Sizing the
	// body limit on the raw text would reject payloads that are within
	// max_text_bytes once decoded.
	ghostboardJSONEscapeFactor = 6

	// ghostboardFormEscapeFactor bounds the same growth for a form field, where
	// percent-encoding expands one byte to three ("%0A").
	ghostboardFormEscapeFactor = 3

	// ghostboardPINBodyLimit bounds the body of an unlock request, which
	// carries a PIN and nothing else.
	ghostboardPINBodyLimit = 512

	// ghostboardCookiePath is the path of the session cookie: the board API is
	// mounted at the root, under /ghostboard and /api/boards.
	ghostboardCookiePath = "/"

	// ghostboardModeHeader and ghostboardBurnHeader let a script ask for a
	// sealed board on the raw-text API, which has no JSON body to carry the
	// mode and the burn flag.
	ghostboardModeHeader = "X-Board-Mode"
	ghostboardBurnHeader = "X-Board-Burn"
)

// errGhostboardInvalidBody marks a payload that could not be decoded.
var errGhostboardInvalidBody = errors.New("invalid request body")

// errGhostboardInvalidText marks a text body that is not valid UTF-8, which
// would be mangled by the JSON persistence of the store.
var errGhostboardInvalidText = errors.New("request body is not valid UTF-8")

// errGhostboardInvalidMode marks a board mode that is not "plain" or "sealed".
var errGhostboardInvalidMode = errors.New("invalid board mode: use plain or sealed")

// boardWrite is one decoded write request: the text plus the mode and the burn
// flag the client asked for. An empty mode is a plain write, which is what every
// client that predates sealed boards sends.
type boardWrite struct {
	Text     string
	ClientID string
	Mode     string
	Burn     bool
}

// GhostboardController exposes the shared text boards over HTTP.
type GhostboardController struct {
	auth         *ghostboard.Authenticator
	store        *ghostboard.Store
	maxTextBytes int
	heartbeat    time.Duration
}

// NewGhostboardController creates a controller for the given store. auth is the
// PIN gate of the private boards and may be nil when no PIN is configured.
func NewGhostboardController(store *ghostboard.Store, auth *ghostboard.Authenticator, maxTextBytes int, heartbeat time.Duration) *GhostboardController {
	return &GhostboardController{
		store:        store,
		auth:         auth,
		maxTextBytes: maxTextBytes,
		heartbeat:    heartbeat,
	}
}

// Get returns the current text of a board.
func (gc *GhostboardController) Get(c *gin.Context) {
	board, err := gc.store.Get(ghostboardBoardName(c))
	if err != nil {
		writeGhostboardError(c, err)
		return
	}

	// A cached envelope would let a burned board outlive its burn, so a sealed
	// board is never stored by a proxy or a browser.
	if board.Mode == ghostboard.ModeSealed {
		c.Header("Cache-Control", "no-store")
	}
	c.JSON(http.StatusOK, board)
}

// Update stores a new text for a board. The body can be a JSON object with a
// "text" field, a form field named "text", or the raw text itself. A sealed
// board is written with the envelope in the same field and the mode asked for
// with the X-Board-Mode header or the "mode" JSON field.
func (gc *GhostboardController) Update(c *gin.Context) {
	name := ghostboardBoardName(c)
	if err := ghostboard.ValidateBoardName(name); err != nil {
		writeGhostboardError(c, err)
		return
	}

	write, err := gc.readWrite(c)
	if err != nil {
		writeGhostboardError(c, err)
		return
	}

	board, err := gc.storeWrite(name, write)
	if err != nil {
		writeGhostboardError(c, err)
		return
	}

	c.JSON(http.StatusOK, board)
}

// storeWrite routes a write to the sealed or the plain path of the store, so the
// two surfaces cannot disagree about which one a request meant.
func (gc *GhostboardController) storeWrite(name string, write boardWrite) (ghostboard.Board, error) {
	mode, err := boardMode(write.Mode)
	if err != nil {
		return ghostboard.Board{}, err
	}
	if mode == ghostboard.ModeSealed {
		return gc.store.UpdateSealed(ghostboard.SealedWrite{
			Name:     name,
			Envelope: write.Text,
			ClientID: write.ClientID,
			Burn:     write.Burn,
		})
	}
	return gc.store.Update(name, write.Text, write.ClientID)
}

// Open hands out the envelope of a burn board once and destroys it. Only a POST
// reaches it: a GET would be consumed by a link preview or a browser prefetch
// before the reader ever saw the note.
func (gc *GhostboardController) Open(c *gin.Context) {
	board, err := gc.store.Open(ghostboardBoardName(c))
	if err != nil {
		writeGhostboardError(c, err)
		return
	}

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, board)
}

// OpenText is Open on the plain API: the envelope alone, for a script that
// decrypts it with gb-cli.
func (gc *GhostboardController) OpenText(c *gin.Context) {
	board, err := gc.store.Open(ghostboardBoardName(c))
	if err != nil {
		writeGhostboardError(c, err)
		return
	}

	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(board.Text))
}

// GetText returns a board's text as plain text for the CLI API. A board that
// was never written is served as an empty body with status 200, so a script
// can treat a missing board exactly like a cleared one.
func (gc *GhostboardController) GetText(c *gin.Context) {
	board, err := gc.store.Get(ghostboardBoardName(c))
	if err != nil {
		writeGhostboardError(c, err)
		return
	}

	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(board.Text))
}

// SetText replaces a board's text with the raw request body. The bytes are
// stored exactly as received: no JSON, form or newline handling. A successful
// write answers 204 No Content.
func (gc *GhostboardController) SetText(c *gin.Context) {
	name := ghostboardBoardName(c)
	if err := ghostboard.ValidateBoardName(name); err != nil {
		writeGhostboardError(c, err)
		return
	}

	write, err := gc.readRawWrite(c)
	if err != nil {
		writeGhostboardError(c, err)
		return
	}

	if _, err := gc.storeWrite(name, write); err != nil {
		writeGhostboardError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// Events streams board changes as Server-Sent Events. The first event carries
// the current snapshot, and a keep-alive comment is sent periodically so
// proxies do not close the idle connection.
func (gc *GhostboardController) Events(c *gin.Context) {
	name := ghostboardBoardName(c)

	sub, snapshot, err := gc.store.Subscribe(name)
	if err != nil {
		writeGhostboardError(c, err)
		return
	}
	defer sub.Close()

	// The server sets an absolute write deadline per request; long-lived
	// streams must clear it or the connection would be closed mid-stream.
	controller := http.NewResponseController(c.Writer)
	if err := controller.SetWriteDeadline(time.Time{}); err != nil {
		logger.WithComponent("ghostboard").Warnf("cannot clear write deadline for board %q stream: %v", name, err)
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	if !writeGhostboardEvent(controller, c, ghostboard.Event{Board: snapshot}) {
		return
	}

	ticker := time.NewTicker(gc.heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case event, ok := <-sub.Events():
			if !ok {
				return
			}
			// A stream is authorised once, when it opens. The session is
			// checked before every write, so the text of a private board never
			// travels on a token that has expired, and on every heartbeat so the
			// stream does not stay open after it.
			if gc.sessionExpired(c, name) {
				writeGhostboardLocked(controller, c)
				return
			}
			if !writeGhostboardEvent(controller, c, event) {
				return
			}
		case <-ticker.C:
			if gc.sessionExpired(c, name) {
				writeGhostboardLocked(controller, c)
				return
			}
			if _, err := fmt.Fprint(c.Writer, ": keep-alive\n\n"); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
	}
}

// Unlock verifies the shared PIN and starts a session. The token is returned in
// the response body for gb-cli and shell scripts and is also set as an HttpOnly
// cookie, which is what the browser page sends: EventSource and
// navigator.sendBeacon cannot attach a header.
func (gc *GhostboardController) Unlock(c *gin.Context) {
	if gc.auth == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": ghostboard.ErrPINNotConfigured.Error()})
		return
	}

	// Only a private board has a PIN to trade. Answering an unlock for a public
	// board would hand out a session for boards this endpoint never protected.
	board := ghostboardBoardName(c)
	if !gc.auth.IsPrivate(board) {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("board %q is not protected by a pin", board)})
		return
	}

	pin, err := gc.readPIN(c)
	if err != nil {
		writeGhostboardError(c, err)
		return
	}

	result, err := gc.auth.Unlock(clientAddress(c), pin)
	if err != nil {
		gc.writeUnlockError(c, err)
		return
	}

	gc.setSessionCookie(c, result.Token)
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"token": result.Token, "expires_at": result.Expires.UTC()})
}

// Lock ends the session of the browser by expiring its cookie. A bearer token a
// script already holds stays valid until it expires, since it is stateless.
func (gc *GhostboardController) Lock(c *gin.Context) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(ghostboard.SessionCookie, "", -1, ghostboardCookiePath, "", requestIsSecure(c), true)
	c.Status(http.StatusNoContent)
}

// readPIN reads the PIN of an unlock request, from a JSON object or a form
// field named "pin". Surrounding spaces are dropped so a PIN copied from a
// terminal is accepted; gb-cli trims the value before hashing it too.
func (gc *GhostboardController) readPIN(c *gin.Context) (string, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, ghostboardPINBodyLimit)

	var payload struct {
		PIN string `json:"pin" form:"pin"`
	}
	if err := c.ShouldBind(&payload); err != nil {
		return "", ghostboardBodyError(err)
	}
	return strings.TrimSpace(payload.PIN), nil
}

// writeUnlockError maps a failed unlock to a stable status and body. A rejected
// attempt is also logged, with the board and the client address but never the
// PIN: the value that failed is a guess of a secret and may be a typo of a
// credential used elsewhere, so it must not reach the log.
func (gc *GhostboardController) writeUnlockError(c *gin.Context, err error) {
	board := ghostboardBoardName(c)
	var attemptsErr *ghostboard.AttemptsError
	switch {
	case errors.As(err, &attemptsErr):
		retryAfter := int(attemptsErr.RetryAfter.Round(time.Second).Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
		logger.WithComponent("ghostboard").Warnf("unlock of private board %q refused for %s: still locked out for %ds", board, clientAddress(c), retryAfter)
		c.Header("Retry-After", strconv.Itoa(retryAfter))
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many pin attempts", "retry_after_secs": retryAfter})
	case errors.Is(err, ghostboard.ErrInvalidPIN):
		logger.WithComponent("ghostboard").Warnf("unlock of private board %q failed for %s: invalid pin", board, clientAddress(c))
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid pin"})
	default:
		writeGhostboardError(c, err)
	}
}

// sessionExpired reports whether the session that opened a private board's
// stream is no longer valid. It is false for a public board and when no PIN is
// configured, where the stream needs no session at all.
func (gc *GhostboardController) sessionExpired(c *gin.Context, board string) bool {
	if gc.auth == nil || !gc.auth.IsPrivate(board) {
		return false
	}
	return !gc.auth.ValidToken(ghostboard.SessionToken(c.Request))
}

// writeGhostboardLocked tells the page that the session of its stream expired,
// so the PIN form comes back instead of a connection that silently stops.
func writeGhostboardLocked(controller *http.ResponseController, c *gin.Context) {
	if _, err := fmt.Fprint(c.Writer, "event: locked\ndata: {\"error\":\"session expired\"}\n\n"); err != nil {
		return
	}
	if err := controller.Flush(); err != nil {
		logger.WithComponent("ghostboard").Debugf("cannot flush the expiry event: %v", err)
	}
}

// setSessionCookie hands the browser its session token.
func (gc *GhostboardController) setSessionCookie(c *gin.Context, token string) {
	maxAge := int(gc.auth.SessionTTL().Round(time.Second).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(ghostboard.SessionCookie, token, maxAge, ghostboardCookiePath, "", requestIsSecure(c), true)
}

// bodyLimit returns the largest body accepted for a content type, derived from
// the configured board size so escaped payloads are not rejected while the text
// they decode to is still within max_text_bytes.
func (gc *GhostboardController) bodyLimit(contentType string) int64 {
	factor := int64(1)
	switch contentType {
	case "application/json":
		factor = ghostboardJSONEscapeFactor
	case "application/x-www-form-urlencoded":
		factor = ghostboardFormEscapeFactor
	}
	return int64(gc.maxTextBytes)*factor + ghostboardBodyOverhead
}

// readWrite decodes a write request: the text from a JSON object, a form field or
// the raw body, and the mode and burn flag from the body or the headers. The
// headers are read first so the raw API, which has no JSON to carry them, can
// ask for a sealed board too.
func (gc *GhostboardController) readWrite(c *gin.Context) (boardWrite, error) {
	write := boardWrite{
		Mode:     strings.TrimSpace(c.GetHeader(ghostboardModeHeader)),
		Burn:     strings.EqualFold(strings.TrimSpace(c.GetHeader(ghostboardBurnHeader)), "true"),
		ClientID: ghostboardClientID(c),
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, gc.bodyLimit(c.ContentType()))

	switch c.ContentType() {
	case "application/json":
		var payload struct {
			Text string `json:"text"`
			Mode string `json:"mode"`
			Burn bool   `json:"burn"`
		}
		if err := c.ShouldBindJSON(&payload); err != nil {
			return boardWrite{}, ghostboardBodyError(err)
		}
		write.Text = payload.Text
		if mode := strings.TrimSpace(payload.Mode); mode != "" {
			write.Mode = mode
		}
		if payload.Burn {
			write.Burn = true
		}
	case "application/x-www-form-urlencoded":
		// A body that is not a text= form field is stored as-is, matching the
		// upstream Ghostboard behaviour for plain curl -d payloads.
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			return boardWrite{}, ghostboardBodyError(err)
		}
		write.Text = string(body)
		if values, err := url.ParseQuery(write.Text); err == nil {
			if texts, ok := values["text"]; ok && len(texts) > 0 {
				write.Text = texts[0]
			}
		}
	default:
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			return boardWrite{}, ghostboardBodyError(err)
		}
		write.Text = string(body)
	}

	// The limit is enforced on the decoded text, so a body padded with JSON
	// escapes cannot bypass it and an escaped body is not rejected for its size.
	if len(write.Text) > gc.maxTextBytes {
		return boardWrite{}, ghostboard.ErrTextTooLarge
	}

	// Boards hold text: invalid UTF-8 would be replaced by U+FFFD when the
	// store marshals the board to its JSON file, silently rewriting what the
	// client sent. JSON decoding already yields valid strings, so this rejects
	// raw and percent-decoded bodies.
	if !utf8.ValidString(write.Text) {
		return boardWrite{}, errGhostboardInvalidText
	}

	return write, nil
}

// readRawWrite reads the whole request body verbatim, enforcing the configured
// board size on the bytes received. Boards hold text, so a body that is not
// valid UTF-8 is rejected instead of being stored and later mangled by the
// JSON persistence.
func (gc *GhostboardController) readRawWrite(c *gin.Context) (boardWrite, error) {
	write := boardWrite{
		Mode:     strings.TrimSpace(c.GetHeader(ghostboardModeHeader)),
		Burn:     strings.EqualFold(strings.TrimSpace(c.GetHeader(ghostboardBurnHeader)), "true"),
		ClientID: ghostboardClientID(c),
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(gc.maxTextBytes))

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return boardWrite{}, ghostboardBodyError(err)
	}
	if !utf8.Valid(body) {
		return boardWrite{}, errGhostboardInvalidText
	}
	write.Text = string(body)
	return write, nil
}

// boardMode maps the mode a client asked for onto a store mode. Anything else is
// refused instead of being treated as plain, so a typo in a client can never
// store ciphertext as if it were readable text.
func boardMode(mode string) (ghostboard.Mode, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", string(ghostboard.ModePlain):
		return ghostboard.ModePlain, nil
	case string(ghostboard.ModeSealed):
		return ghostboard.ModeSealed, nil
	default:
		return "", errGhostboardInvalidMode
	}
}

func writeGhostboardEvent(controller *http.ResponseController, c *gin.Context, event ghostboard.Event) bool {
	payload, err := json.Marshal(event)
	if err != nil {
		logger.WithComponent("ghostboard").Errorf("cannot encode event for board %q: %v", event.Name, err)
		return false
	}

	if _, err := fmt.Fprintf(c.Writer, "event: board\ndata: %s\n\n", payload); err != nil {
		return false
	}
	if err := controller.Flush(); err != nil {
		logger.WithComponent("ghostboard").Debugf("cannot flush event for board %q: %v", event.Name, err)
		return false
	}
	return true
}

// clientAddress is the address the request came from. It deliberately ignores
// X-Forwarded-For: the header is client-controlled, and trusting it would let a
// spoofed value bypass the attempt limiter.
func clientAddress(c *gin.Context) string {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return c.Request.RemoteAddr
	}
	return host
}

// requestIsSecure reports whether the browser reached the server over HTTPS, so
// the session cookie can carry the Secure flag. A reverse proxy terminates TLS,
// so its X-Forwarded-Proto header is honoured.
func requestIsSecure(c *gin.Context) bool {
	return c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
}

func ghostboardBoardName(c *gin.Context) string {
	name := strings.TrimSpace(c.Param("board"))
	if name == "" {
		return ghostboard.DefaultBoard
	}
	return name
}

func ghostboardClientID(c *gin.Context) string {
	clientID := strings.TrimSpace(c.GetHeader(ghostboardClientHeader))
	if len(clientID) > ghostboardClientIDMax {
		return clientID[:ghostboardClientIDMax]
	}
	return clientID
}

func ghostboardBodyError(err error) error {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return ghostboard.ErrTextTooLarge
	}
	return fmt.Errorf("%w: %v", errGhostboardInvalidBody, err)
}

func writeGhostboardError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ghostboard.ErrInvalidBoardName):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid board name: use 1-64 letters, digits, dots, dashes or underscores"})
	case errors.Is(err, ghostboard.ErrBoardBurned):
		c.JSON(http.StatusGone, gin.H{"error": ghostboard.ErrBoardBurned.Error()})
	case errors.Is(err, ghostboard.ErrBoardNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": ghostboard.ErrBoardNotFound.Error()})
	case errors.Is(err, ghostboard.ErrSealedDisabled):
		c.JSON(http.StatusNotFound, gin.H{"error": ghostboard.ErrSealedDisabled.Error()})
	case errors.Is(err, ghostboard.ErrBurnRequiresOpen):
		c.JSON(http.StatusConflict, gin.H{"error": ghostboard.ErrBurnRequiresOpen.Error(), "open": "POST /ghostboard/" + ghostboardBoardName(c) + "/open"})
	case errors.Is(err, ghostboard.ErrBoardNotSealed):
		c.JSON(http.StatusConflict, gin.H{"error": ghostboard.ErrBoardNotSealed.Error()})
	case errors.Is(err, ghostboard.ErrPlaintextWriteToSealed):
		c.JSON(http.StatusConflict, gin.H{"error": ghostboard.ErrPlaintextWriteToSealed.Error()})
	case errors.Is(err, ghostboard.ErrSealedOverPlainText):
		c.JSON(http.StatusConflict, gin.H{"error": ghostboard.ErrSealedOverPlainText.Error()})
	case errors.Is(err, ghostboard.ErrSealedBoardFiles):
		c.JSON(http.StatusConflict, gin.H{"error": ghostboard.ErrSealedBoardFiles.Error()})
	case errors.Is(err, errGhostboardInvalidMode):
		c.JSON(http.StatusBadRequest, gin.H{"error": errGhostboardInvalidMode.Error()})
	case errors.Is(err, sealedcrypto.ErrInvalidEnvelope), errors.Is(err, sealedcrypto.ErrUnsupportedVersion):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid sealed envelope: expected {\"v\":1,\"iv\":…,\"ct\":…} in base64url"})
	case errors.Is(err, ghostboard.ErrTextTooLarge):
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": err.Error()})
	case errors.Is(err, ghostboard.ErrPINLength):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, errGhostboardInvalidText):
		c.JSON(http.StatusBadRequest, gin.H{"error": "request body is not valid UTF-8: boards hold text only"})
	case errors.Is(err, errGhostboardInvalidBody):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
	default:
		logger.WithComponent("ghostboard").Errorf("ghostboard request failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}
