package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bassista/go_spin/internal/ghostboard"
	"github.com/bassista/go_spin/internal/logger"
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
)

// errGhostboardInvalidBody marks a payload that could not be decoded.
var errGhostboardInvalidBody = errors.New("invalid request body")

// GhostboardController exposes the shared text boards over HTTP.
type GhostboardController struct {
	store        *ghostboard.Store
	maxTextBytes int
	heartbeat    time.Duration
}

// NewGhostboardController creates a controller for the given store.
func NewGhostboardController(store *ghostboard.Store, maxTextBytes int, heartbeat time.Duration) *GhostboardController {
	return &GhostboardController{
		store:        store,
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
	return text, nil
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
	case errors.Is(err, errGhostboardInvalidBody):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
	default:
		logger.WithComponent("ghostboard").Errorf("ghostboard request failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}
