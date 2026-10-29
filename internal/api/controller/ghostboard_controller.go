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
)

// errGhostboardInvalidBody marks a payload that could not be decoded.
var errGhostboardInvalidBody = errors.New("invalid request body")

// errGhostboardInvalidText marks a text body that is not valid UTF-8, which
// would be mangled by the JSON persistence of the store.
var errGhostboardInvalidText = errors.New("request body is not valid UTF-8")

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
	c.JSON(http.StatusOK, board)
}

// Update stores a new text for a board. The body can be a JSON object with a
// "text" field, a form field named "text", or the raw text itself.
func (gc *GhostboardController) Update(c *gin.Context) {
	name := ghostboardBoardName(c)
	if err := ghostboard.ValidateBoardName(name); err != nil {
		writeGhostboardError(c, err)
		return
	}

	text, err := gc.readText(c)
	if err != nil {
		writeGhostboardError(c, err)
		return
	}

	board, err := gc.store.Update(name, text, ghostboardClientID(c))
	if err != nil {
		writeGhostboardError(c, err)
		return
	}

	c.JSON(http.StatusOK, board)
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

	text, err := gc.readRawText(c)
	if err != nil {
		writeGhostboardError(c, err)
		return
	}

	if _, err := gc.store.Update(name, text, ghostboardClientID(c)); err != nil {
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
			if !writeGhostboardEvent(controller, c, event) {
				return
			}
		case <-ticker.C:
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

// writeUnlockError maps a failed unlock to a stable status and body.
func (gc *GhostboardController) writeUnlockError(c *gin.Context, err error) {
	var attemptsErr *ghostboard.AttemptsError
	switch {
	case errors.As(err, &attemptsErr):
		retryAfter := int(attemptsErr.RetryAfter.Round(time.Second).Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
		c.Header("Retry-After", strconv.Itoa(retryAfter))
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many pin attempts", "retry_after_secs": retryAfter})
	case errors.Is(err, ghostboard.ErrInvalidPIN):
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid pin"})
	default:
		writeGhostboardError(c, err)
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

func (gc *GhostboardController) readText(c *gin.Context) (string, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, gc.bodyLimit(c.ContentType()))

	var text string
	switch c.ContentType() {
	case "application/json":
		var payload struct {
			Text string `json:"text"`
		}
		if err := c.ShouldBindJSON(&payload); err != nil {
			return "", ghostboardBodyError(err)
		}
		text = payload.Text
	case "application/x-www-form-urlencoded":
		// A body that is not a text= form field is stored as-is, matching the
		// upstream Ghostboard behaviour for plain curl -d payloads.
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			return "", ghostboardBodyError(err)
		}
		text = string(body)
		if values, err := url.ParseQuery(text); err == nil {
			if texts, ok := values["text"]; ok && len(texts) > 0 {
				text = texts[0]
			}
		}
	default:
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			return "", ghostboardBodyError(err)
		}
		text = string(body)
	}

	// The limit is enforced on the decoded text, so a body padded with JSON
	// escapes cannot bypass it and an escaped body is not rejected for its size.
	if len(text) > gc.maxTextBytes {
		return "", ghostboard.ErrTextTooLarge
	}

	// Boards hold text: invalid UTF-8 would be replaced by U+FFFD when the
	// store marshals the board to its JSON file, silently rewriting what the
	// client sent. JSON decoding already yields valid strings, so this rejects
	// raw and percent-decoded bodies.
	if !utf8.ValidString(text) {
		return "", errGhostboardInvalidText
	}

	return text, nil
}

// readRawText reads the whole request body verbatim, enforcing the configured
// board size on the bytes received. Boards hold text, so a body that is not
// valid UTF-8 is rejected instead of being stored and later mangled by the
// JSON persistence.
func (gc *GhostboardController) readRawText(c *gin.Context) (string, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(gc.maxTextBytes))

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return "", ghostboardBodyError(err)
	}
	if !utf8.Valid(body) {
		return "", errGhostboardInvalidText
	}
	return string(body), nil
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
