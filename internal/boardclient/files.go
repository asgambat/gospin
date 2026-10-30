package boardclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/asgambat/gospin/internal/ghostboard"
	"github.com/asgambat/gospin/internal/logger"
)

const (
	// fileEndpoint names the attachment routes in the messages about a missing
	// route.
	fileEndpoint = "/api/boards/:board/files"
)

// ErrFileNotFound is returned when the server does not know an attachment id.
// It is distinct from EndpointError: the route exists and answered, the file
// does not.
var ErrFileNotFound = errors.New("file not found")

// File is one attachment of a board: the opaque id used in URLs, the original
// name kept as a download label and the size and expiry the server reports.
//
// The timestamps are pointers so a listing that omits them does not serialise
// a zero time.
type File struct {
	CreatedAt   *time.Time `json:"created_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	ContentType string     `json:"content_type,omitempty"`
	Size        int64      `json:"size"`
}

// ListFiles returns the attachments of a board, newest first.
func (c *Client) ListFiles(ctx context.Context, board string) ([]File, error) {
	payload, err := c.fetch(ctx, c.filesURL(board), board, fileEndpoint, "listing the files of")
	if err != nil {
		return nil, err
	}
	return parseFileList(string(payload))
}

// UploadFile sends one file to the board and returns what the server stored.
// The bytes are streamed, so a file of hundreds of megabytes is not held in
// memory. A PIN, when configured, is traded for a session before the transfer:
// a stream cannot be replayed after a 401.
func (c *Client) UploadFile(ctx context.Context, board, name string, content io.Reader) (File, error) {
	if err := ghostboard.ValidateBoardName(board); err != nil {
		return File{}, &BoardNameError{Name: board}
	}
	if c.pin != "" {
		if err := c.ensureSession(ctx, board); err != nil {
			return File{}, err
		}
	}

	resp, err := c.uploadOnce(ctx, board, name, content)
	if err != nil {
		return File{}, err
	}
	defer closeBody(resp, board)

	if resp.StatusCode == http.StatusUnauthorized {
		return File{}, &PINError{Board: board}
	}
	if resp.StatusCode != http.StatusCreated {
		return File{}, responseError(resp, board, "uploading a file to")
	}

	payload, err := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
	if err != nil {
		return File{}, fmt.Errorf("reading the upload answer of board %q: %w", board, err)
	}
	return parseFileLine(string(payload))
}

// DownloadFile streams an attachment to w and returns how many bytes it wrote.
func (c *Client) DownloadFile(ctx context.Context, board, id string, w io.Writer) (int64, error) {
	if err := ghostboard.ValidateBoardName(board); err != nil {
		return 0, &BoardNameError{Name: board}
	}
	if strings.TrimSpace(id) == "" {
		return 0, errors.New("file id is empty")
	}
	if c.pin != "" {
		if err := c.ensureSession(ctx, board); err != nil {
			return 0, err
		}
	}

	requestURL := c.fileURL(board, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return 0, fmt.Errorf("building the download request for board %q: %w", board, err)
	}
	c.applyHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("contacting the GoSpin server at %s: %w", c.baseURL, err)
	}
	defer closeBody(resp, board)

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return 0, &PINError{Board: board}
	case http.StatusNotFound:
		// A missing route and a missing file both answer 404: the body is what
		// tells them apart, and only the second one is the caller's mistake.
		if isMissingFile(resp) {
			return 0, fmt.Errorf("%w: %s", ErrFileNotFound, id)
		}
		return 0, &EndpointError{URL: requestURL, Endpoint: fileEndpoint}
	default:
		return 0, responseError(resp, board, "downloading a file from")
	}

	written, err := io.Copy(w, resp.Body)
	if err != nil {
		return written, fmt.Errorf("writing the file of board %q: %w", board, err)
	}
	return written, nil
}

// DeleteFile removes one attachment from the board. An unknown id is reported as
// ErrFileNotFound, so the caller can tell a typo or an expired file from a
// server that does not expose the attachment routes at all.
func (c *Client) DeleteFile(ctx context.Context, board, id string) error {
	if err := ghostboard.ValidateBoardName(board); err != nil {
		return &BoardNameError{Name: board}
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("file id is empty")
	}
	if c.pin != "" {
		if err := c.ensureSession(ctx, board); err != nil {
			return err
		}
	}

	requestURL := c.fileURL(board, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, requestURL, nil)
	if err != nil {
		return fmt.Errorf("building the delete request for board %q: %w", board, err)
	}
	c.applyHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("contacting the GoSpin server at %s: %w", c.baseURL, err)
	}
	defer closeBody(resp, board)

	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return &PINError{Board: board}
	case http.StatusNotFound:
		// As for a download, a missing route and a missing file both answer
		// 404: only the body tells them apart.
		if isMissingFile(resp) {
			return fmt.Errorf("%w: %s", ErrFileNotFound, id)
		}
		return &EndpointError{URL: requestURL, Endpoint: fileEndpoint}
	default:
		return responseError(resp, board, "deleting a file from")
	}
}

// ensureSession trades the PIN for a session token before a streaming request.
// A public board answers the unlock with 404: there is no PIN to trade, and the
// request that follows is already authorised.
func (c *Client) ensureSession(ctx context.Context, board string) error {
	c.sessionMu.Lock()
	hasSession := c.session != ""
	c.sessionMu.Unlock()
	if hasSession {
		return nil
	}

	err := c.unlock(ctx, board)
	var responseErr *ResponseError
	if errors.As(err, &responseErr) && responseErr.Status == http.StatusNotFound {
		return nil
	}
	return err
}

// uploadOnce performs one multipart upload. The body is produced by a pipe, so
// the file is streamed instead of buffered; that also means the request cannot
// be retried, which is why the session is obtained beforehand.
func (c *Client) uploadOnce(ctx context.Context, board, name string, content io.Reader) (*http.Response, error) {
	reader, writer := io.Pipe()
	form := multipart.NewWriter(writer)
	contentType := form.FormDataContentType()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.filesURL(board), reader)
	if err != nil {
		if closeErr := writer.Close(); closeErr != nil {
			logger.WithComponent("boardclient").Debugf("closing the upload pipe: %v", closeErr)
		}
		return nil, fmt.Errorf("building the upload request for board %q: %w", board, err)
	}
	req.Header.Set("Content-Type", contentType)
	c.applyHeaders(req)

	go func() {
		part, err := form.CreateFormFile("file", name)
		if err == nil {
			_, err = io.Copy(part, content)
		}
		if closeErr := form.Close(); err == nil {
			err = closeErr
		}
		// Closing the writer with the error hands it to the request, so a read
		// failure of the local file aborts the transfer instead of sending a
		// truncated one.
		_ = writer.CloseWithError(err)
	}()

	resp, err := c.http.Do(req)
	if err != nil {
		// The pipe writer is still blocked on a reader nobody will drain.
		if closeErr := reader.Close(); closeErr != nil {
			logger.WithComponent("boardclient").Debugf("closing the upload pipe: %v", closeErr)
		}
		return nil, fmt.Errorf("uploading to board %q at %s: %w", board, c.baseURL, err)
	}
	return resp, nil
}

// parseFileList decodes the tab-separated listing of a board.
func parseFileList(body string) ([]File, error) {
	files := []File{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		file, err := parseFileLine(line)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

// parseFileLine decodes one line of the plain listing:
// id, name, size in bytes and expiry.
func parseFileLine(line string) (File, error) {
	fields := strings.Split(strings.TrimRight(line, "\r\n"), "\t")
	if len(fields) != 4 {
		return File{}, fmt.Errorf("unexpected file listing %q", line)
	}

	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return File{}, fmt.Errorf("unexpected file size %q: %w", fields[2], err)
	}
	expires, err := time.Parse(time.RFC3339, fields[3])
	if err != nil {
		return File{}, fmt.Errorf("unexpected file expiry %q: %w", fields[3], err)
	}

	return File{
		ID:        fields[0],
		Name:      fields[1],
		Size:      size,
		ExpiresAt: &expires,
	}, nil
}

// isMissingFile reports whether a 404 carries the message of an unknown
// attachment, as opposed to the answer of a server without the file routes.
func isMissingFile(resp *http.Response) bool {
	if isHTML(resp.Header.Get("Content-Type")) {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
	if err != nil {
		return false
	}
	return errorMessage(body) == ghostboard.ErrFileNotFound.Error()
}

// filesURL builds the plain API URL of the attachments of a board.
func (c *Client) filesURL(board string) string {
	return c.baseURL + "/" + boardsPath + "/" + url.PathEscape(board) + "/files"
}

// fileURL builds the URL of one attachment.
func (c *Client) fileURL(board, id string) string {
	return c.filesURL(board) + "/" + url.PathEscape(id)
}
