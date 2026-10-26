// Package boardclient is the HTTP client behind the gb-cli CLI. It talks
// to the plain-text board API (/api/boards/:board) served by GoSpin, so the CLI
// needs no access to the server's files and works against a remote instance.
package boardclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bassista/go_spin/internal/ghostboard"
	"github.com/bassista/go_spin/internal/logger"
)

const (
	// boardsPath is the plain-text board API exposed by the GoSpin server.
	boardsPath = "api/boards"

	// statePath is the JSON board API, which also carries the revision.
	statePath = "ghostboard"

	// boardEndpoint, stateEndpoint and eventEndpoint name the routes the CLI
	// depends on in the messages about a missing route.
	boardEndpoint = "/api/boards/:board"
	stateEndpoint = "/ghostboard/:board"
	eventEndpoint = "/ghostboard/:board/events"

	// errorBodyLimit bounds how much of an unexpected response body is quoted
	// in an error message.
	errorBodyLimit = 512

	// DefaultRetryDelay is the pause before the first retry; it doubles on every
	// further attempt, up to maxRetryDelay.
	DefaultRetryDelay = 200 * time.Millisecond

	// maxRetryDelay caps the exponential backoff between two attempts.
	maxRetryDelay = 2 * time.Second
)

// ErrBoardName is matched by BoardNameError, so a caller can tell an invalid
// board name from a transport or server failure.
var ErrBoardName = errors.New("invalid board name")

// ResponseError describes a non-success answer from the server.
type ResponseError struct {
	Board   string `json:"-"`
	Action  string `json:"-"`
	Message string `json:"error"`
	Status  int    `json:"-"`
}

// Error renders the failure with the action, the board and the server message.
func (e *ResponseError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("%s board %q: server answered %d", e.Action, e.Board, e.Status)
	}
	return fmt.Sprintf("%s board %q: server answered %d: %s", e.Action, e.Board, e.Status, e.Message)
}

// EndpointError reports a 404 on a board route: the URL does not point at a
// GoSpin board API. Only GoSpin serves those paths, so a missing route means
// the request went somewhere else (wrong port, host or path prefix, or a GoSpin
// release without the route) far more often than it means a missing board: the
// board API answers an unknown board with an empty 200.
type EndpointError struct {
	URL      string
	Endpoint string
}

// Error describes the missing route and points at the URL that was requested.
func (e *EndpointError) Error() string {
	return fmt.Sprintf("404 Not Found on %s: the server does not expose %s; check --url and the configuration file, and that the server runs a GoSpin release that serves the board API", e.URL, e.Endpoint)
}

// BoardNameError reports a board name that cannot address a board.
type BoardNameError struct {
	Name string
}

// Error explains which names are accepted.
func (e *BoardNameError) Error() string {
	return fmt.Sprintf("invalid board name %q: use 1-64 letters, digits, dots, dashes or underscores", e.Name)
}

// Unwrap lets callers match the failure with errors.Is(ErrBoardName).
func (e *BoardNameError) Unwrap() error {
	return ErrBoardName
}

// Board is the state carried by the JSON API and the event stream: the whole
// text plus the revision and the last update time.
//
// UpdatedAt is a pointer so a board that was never written omits the field
// instead of serialising a timestamp in year 1.
type Board struct {
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	Name      string     `json:"name"`
	Text      string     `json:"text"`
	Revision  uint64     `json:"revision"`
}

// normalizeBoard drops the zero update time the server sends for a board that
// was never written, so a consumer sees "no update yet" instead of 0001-01-01.
func normalizeBoard(board Board) Board {
	if board.UpdatedAt != nil && board.UpdatedAt.IsZero() {
		board.UpdatedAt = nil
	}
	return board
}

// Options tunes a Client. Only Timeout is required.
type Options struct {
	// Headers are sent with every request, for a reverse proxy that requires
	// authentication.
	Headers http.Header
	// Timeout bounds a single request.
	Timeout time.Duration
	// Retries is the number of extra attempts after a transient failure.
	Retries int
	// RetryDelay is the pause before the first retry; it doubles on every
	// further attempt. Zero means DefaultRetryDelay.
	RetryDelay time.Duration
}

// Client reads and writes boards on a running GoSpin server.
type Client struct {
	http       *http.Client
	streamHTTP *http.Client
	headers    http.Header
	baseURL    string
	retryDelay time.Duration
	retries    int
}

// New creates a client for the given server URL with a plain timeout. The URL
// must be http or https and may carry a path prefix when GoSpin sits behind a
// reverse proxy.
func New(baseURL string, timeout time.Duration) (*Client, error) {
	return NewWithOptions(baseURL, Options{Timeout: timeout})
}

// NewWithOptions creates a client from a full option set.
func NewWithOptions(baseURL string, opts Options) (*Client, error) {
	normalized, err := NormalizeURL(baseURL)
	if err != nil {
		return nil, err
	}
	if opts.Timeout <= 0 {
		return nil, errors.New("request timeout must be positive")
	}
	if opts.Retries < 0 {
		return nil, errors.New("retries must not be negative")
	}
	if opts.RetryDelay < 0 {
		return nil, errors.New("retry delay must not be negative")
	}

	retryDelay := opts.RetryDelay
	if retryDelay == 0 {
		retryDelay = DefaultRetryDelay
	}

	return &Client{
		baseURL:    normalized,
		headers:    cloneHeader(opts.Headers),
		retries:    opts.Retries,
		retryDelay: retryDelay,
		http:       &http.Client{Timeout: opts.Timeout},
		// The event stream stays open for as long as the user watches it, so it
		// must not carry the per-request timeout.
		streamHTTP: &http.Client{Transport: http.DefaultTransport},
	}, nil
}

// NormalizeURL validates a server base URL and strips the trailing slashes so
// board URLs are built without a double slash.
func NormalizeURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("server URL is empty")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid server URL %q: %w", raw, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("invalid server URL %q: scheme must be http or https", raw)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("invalid server URL %q: missing host", raw)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("invalid server URL %q: must not carry a query or a fragment", raw)
	}
	// /ui/* serves the web pages of GoSpin; the board API lives at the root of
	// the server (or under the prefix a reverse proxy mounts it on), so a page
	// URL would be used as an API prefix and only ever answer 404.
	if isUIPath(parsed.Path) {
		return "", fmt.Errorf("invalid server URL %q: %s is a GoSpin page, not an API base; use the server root, e.g. %s://%s", raw, parsed.Path, parsed.Scheme, parsed.Host)
	}

	return strings.TrimRight(parsed.String(), "/"), nil
}

// isUIPath reports whether a URL path points at the web UI instead of the API.
func isUIPath(path string) bool {
	return path == "/ui" || strings.HasPrefix(path, "/ui/")
}

// Get returns the board text. A board that was never written comes back as an
// empty string, exactly like a board that was cleared.
func (c *Client) Get(ctx context.Context, board string) (string, error) {
	payload, err := c.fetch(ctx, c.boardURL(board), board, boardEndpoint, "reading")
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

// GetBoard returns the board state from the JSON API: the text plus the
// revision and the last update time.
func (c *Client) GetBoard(ctx context.Context, board string) (Board, error) {
	payload, err := c.fetch(ctx, c.stateURL(board), board, stateEndpoint, "reading")
	if err != nil {
		return Board{}, err
	}

	var state Board
	if err := json.Unmarshal(payload, &state); err != nil {
		return Board{}, fmt.Errorf("decoding the state of board %q: %w", board, err)
	}
	return normalizeBoard(state), nil
}

// Set replaces the board text with text, byte for byte. An empty text clears
// the board for every connected client.
func (c *Client) Set(ctx context.Context, board, text string) error {
	_, err := c.send(ctx, http.MethodPost, c.boardURL(board), board, boardEndpoint, []byte(text), "writing")
	return err
}

// Append adds text to the end of the board and returns the resulting text.
//
// The text is separated from the board by a newline when the board does not
// already end with one (see AppendText), so the addition never glues onto the
// last line. Append is a read followed by a write, so a concurrent writer in
// between can be overwritten; Set is the last-writer-wins alternative.
func (c *Client) Append(ctx context.Context, board, text string) (string, error) {
	current, err := c.Get(ctx, board)
	if err != nil {
		return "", err
	}

	updated := AppendText(current, text)
	if err := c.Set(ctx, board, updated); err != nil {
		return "", err
	}
	return updated, nil
}

// AppendText returns the text Append would store for the given board content.
// A board is a text, not a list of lines, so a newline is inserted between the
// two when the board does not already end with one. The separator is skipped
// for an empty board, for an empty addition and when the addition already
// starts with a line break, so repeated appends never produce blank lines.
func AppendText(current, text string) string {
	if text == "" || current == "" || strings.HasSuffix(current, "\n") || strings.HasPrefix(text, "\n") {
		return current + text
	}
	return current + "\n" + text
}

// Follow streams a board until ctx is cancelled, calling handle with the
// current snapshot and then with every change. Each callback carries the whole
// board text, so a caller never has to merge partial payloads. It returns nil
// when the stream ends because ctx was cancelled.
func (c *Client) Follow(ctx context.Context, board string, handle func(Board)) error {
	if err := ghostboard.ValidateBoardName(board); err != nil {
		return &BoardNameError{Name: board}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.eventURL(board), nil)
	if err != nil {
		return fmt.Errorf("building the event request for board %q: %w", board, err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	c.applyHeaders(req)

	resp, err := c.streamHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("contacting the GoSpin server at %s: %w", c.baseURL, err)
	}
	defer closeBody(resp, board)

	if resp.StatusCode == http.StatusNotFound {
		return &EndpointError{URL: c.eventURL(board), Endpoint: eventEndpoint}
	}
	if resp.StatusCode != http.StatusOK {
		return responseError(resp, board, "following")
	}

	// The stream is read line by line without a fixed cap: an event carries the
	// whole board, whose size the server already bounds with
	// ghostboard.max_text_bytes, so a scanner buffer could only disagree with
	// that limit and break on a large board.
	reader := bufio.NewReader(resp.Body)
	for {
		line, readErr := reader.ReadString('\n')
		if payload, ok := eventPayload(strings.TrimRight(line, "\r\n")); ok {
			var state Board
			if err := json.Unmarshal([]byte(payload), &state); err != nil {
				return fmt.Errorf("decoding an event of board %q: %w", board, err)
			}
			handle(normalizeBoard(state))
		}

		if readErr == nil {
			continue
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("reading the event stream of board %q: %w", board, readErr)
	}
}

// fetch performs a GET against a board route, retrying a transient failure.
func (c *Client) fetch(ctx context.Context, requestURL, board, endpoint, action string) ([]byte, error) {
	return c.send(ctx, http.MethodGet, requestURL, board, endpoint, nil, action)
}

// send performs one request, retrying transient failures up to the configured
// number of attempts. The body is rebuilt on every attempt so a retry sends
// exactly what the first attempt did.
func (c *Client) send(ctx context.Context, method, requestURL, board, endpoint string, body []byte, action string) ([]byte, error) {
	if err := ghostboard.ValidateBoardName(board); err != nil {
		return nil, &BoardNameError{Name: board}
	}

	for attempt := 0; ; attempt++ {
		payload, retriable, err := c.attempt(ctx, method, requestURL, board, endpoint, body, action)
		if err == nil {
			return payload, nil
		}
		if !retriable || attempt >= c.retries {
			return nil, err
		}
		if waitErr := waitBeforeRetry(ctx, c.delay(attempt)); waitErr != nil {
			return nil, err
		}
	}
}

// attempt is a single request. It reports whether the failure is worth another
// attempt: a transport error or one of the statuses of isRetriableStatus.
func (c *Client) attempt(ctx context.Context, method, requestURL, board, endpoint string, body []byte, action string) ([]byte, bool, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, reader)
	if err != nil {
		return nil, false, fmt.Errorf("building request for board %q: %w", board, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	}
	c.applyHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		retriable := ctx.Err() == nil
		return nil, retriable, fmt.Errorf("contacting the GoSpin server at %s: %w", c.baseURL, err)
	}
	defer closeBody(resp, board)

	if resp.StatusCode == http.StatusNotFound {
		return nil, false, &EndpointError{URL: requestURL, Endpoint: endpoint}
	}
	if isRetriableStatus(resp.StatusCode) {
		return nil, true, responseError(resp, board, action)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode > http.StatusMultipleChoices-1 {
		return nil, false, responseError(resp, board, action)
	}

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("reading the text of board %q: %w", board, err)
	}
	return payload, false, nil
}

// isRetriableStatus reports whether a status is worth another attempt: the
// server is overloaded or a proxy in front of it failed to reach it.
func isRetriableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// delay returns the backoff before the next attempt.
func (c *Client) delay(attempt int) time.Duration {
	delay := c.retryDelay
	for i := 0; i < attempt; i++ {
		delay *= 2
		if delay >= maxRetryDelay {
			return maxRetryDelay
		}
	}
	return delay
}

// waitBeforeRetry sleeps unless the context ends first.
func waitBeforeRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// applyHeaders copies the configured headers onto a request.
func (c *Client) applyHeaders(req *http.Request) {
	for name, values := range c.headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
}

// boardURL builds the plain-text API URL of a board, escaping the name so a dot
// or a dash never changes the meaning of the path.
func (c *Client) boardURL(board string) string {
	return c.baseURL + "/" + boardsPath + "/" + url.PathEscape(board)
}

// stateURL builds the JSON API URL of a board.
func (c *Client) stateURL(board string) string {
	return c.baseURL + "/" + statePath + "/" + url.PathEscape(board)
}

// eventURL builds the Server-Sent Events URL of a board.
func (c *Client) eventURL(board string) string {
	return c.baseURL + "/" + statePath + "/" + url.PathEscape(board) + "/events"
}

// eventPayload extracts the JSON carried by a Server-Sent Event data line. The
// other fields of the stream (event name, keep-alive comments) are skipped.
func eventPayload(line string) (string, bool) {
	const prefix = "data:"
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, prefix)), true
}

// responseError turns a non-success answer into an error, preferring the JSON
// error envelope of the API. An HTML body (the page of a web server or of a
// reverse proxy) is not quoted: it says nothing about the board and only hides
// the status.
func responseError(resp *http.Response, board, action string) error {
	message := ""
	if !isHTML(resp.Header.Get("Content-Type")) {
		body, err := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
		if err == nil {
			message = errorMessage(body)
		}
	}
	if message == "" {
		message = resp.Status
	}

	return &ResponseError{
		Status:  resp.StatusCode,
		Board:   board,
		Action:  action,
		Message: message,
	}
}

// errorMessage returns the JSON error of the API when there is one, the plain
// text body otherwise, and an empty string when neither carries a message.
func errorMessage(body []byte) string {
	var envelope struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		return strings.TrimSpace(envelope.Error)
	}
	return strings.TrimSpace(string(body))
}

// isHTML reports whether a content type carries a web page, whose body is
// useless in an error message.
func isHTML(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(contentType), "text/html")
}

// cloneHeader copies a header so later changes to the caller's map never reach
// the client.
func cloneHeader(header http.Header) http.Header {
	clone := make(http.Header, len(header))
	for name, values := range header {
		clone[name] = append([]string(nil), values...)
	}
	return clone
}

// closeBody releases a response body, reporting a failure at debug level since
// the request itself already succeeded.
func closeBody(resp *http.Response, board string) {
	if err := resp.Body.Close(); err != nil {
		logger.WithComponent("boardclient").Debugf("closing response body for board %q: %v", board, err)
	}
}
