package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/asgambat/gospin/internal/ghostboard"
	"github.com/asgambat/gospin/internal/logger"
)

const (
	// ghostboardUploadOverhead is how much room the multipart envelope of one
	// file gets on top of max_file_bytes: boundaries, part headers and the
	// filename. Anything beyond it is rejected before it reaches the store.
	ghostboardUploadOverhead = 1 << 20

	// ghostboardDrainLimit bounds how much of the rest of an upload is read
	// after the file part, so a client that appends parts is not cut off while
	// the server does not spend time on bytes it ignores.
	ghostboardDrainLimit = 1 << 20
)

// filesAvailable reports whether the store serves attachments. A disabled
// feature answers 404 on every file route, as if the routes did not exist.
func (gc *GhostboardController) filesAvailable(c *gin.Context) bool {
	if gc.store.FileLimits().Enabled {
		return true
	}
	c.JSON(http.StatusNotFound, gin.H{"error": ghostboard.ErrFilesDisabled.Error()})
	return false
}

// ListFiles returns the attachments of a board.
func (gc *GhostboardController) ListFiles(c *gin.Context) {
	if !gc.filesAvailable(c) {
		return
	}

	files, err := gc.store.Files(ghostboardBoardName(c))
	if err != nil {
		writeGhostboardError(c, err)
		return
	}

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"files": files})
}

// ListFilesPlain returns the attachments of a board as one tab-separated line
// per file, for scripts: id, name, size in bytes and expiry.
func (gc *GhostboardController) ListFilesPlain(c *gin.Context) {
	if !gc.filesAvailable(c) {
		return
	}

	files, err := gc.store.Files(ghostboardBoardName(c))
	if err != nil {
		writeGhostboardError(c, err)
		return
	}

	var body strings.Builder
	for _, file := range files {
		fmt.Fprintf(&body, "%s\t%s\t%d\t%s\n",
			file.ID, filenameLine(file.Name), file.Size, file.ExpiresAt.UTC().Format(time.RFC3339))
	}

	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(body.String()))
}

// UploadFile stores one attachment and answers with its metadata.
func (gc *GhostboardController) UploadFile(c *gin.Context) {
	board := ghostboardBoardName(c)
	file, ok := gc.storeUpload(c, board)
	if !ok {
		return
	}

	c.Header("Cache-Control", "no-store")
	c.Header("Location", fmt.Sprintf("/ghostboard/%s/files/%s", url.PathEscape(board), url.PathEscape(file.ID)))
	c.JSON(http.StatusCreated, file)
}

// UploadFilePlain stores one attachment and answers with its line of the plain
// listing, so a shell script can capture the id without parsing JSON.
func (gc *GhostboardController) UploadFilePlain(c *gin.Context) {
	file, ok := gc.storeUpload(c, ghostboardBoardName(c))
	if !ok {
		return
	}

	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusCreated, "text/plain; charset=utf-8", []byte(fmt.Sprintf("%s\t%s\t%d\t%s\n",
		file.ID, filenameLine(file.Name), file.Size, file.ExpiresAt.UTC().Format(time.RFC3339))))
}

// DownloadFile streams an attachment as a download. It is never served inline:
// an uploaded HTML or SVG document must not run on the dashboard origin, where
// it would carry the PIN session of a private board.
func (gc *GhostboardController) DownloadFile(c *gin.Context) {
	if !gc.filesAvailable(c) {
		return
	}

	limits := gc.store.FileLimits()

	file, handle, err := gc.store.OpenFile(ghostboardBoardName(c), strings.TrimSpace(c.Param("id")))
	if err != nil {
		gc.writeFileError(c, err)
		return
	}
	defer func() {
		if err := handle.Close(); err != nil {
			logger.WithComponent("ghostboard").Debugf("closing attachment %s: %v", file.ID, err)
		}
	}()

	// A large file needs the transfer window, not the per-request timeout of
	// the API routes.
	if err := http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(limits.TransferTimeout)); err != nil {
		logger.WithComponent("ghostboard").Debugf("cannot set the download deadline: %v", err)
	}

	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", ghostboardContentDisposition(file.Name))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Security-Policy", "sandbox")

	// ServeContent handles Range, If-Modified-Since and Content-Length, so a
	// download can resume and a big file is streamed instead of buffered.
	http.ServeContent(c.Writer, c.Request, file.Name, file.CreatedAt, handle)
}

// DeleteFile removes one attachment of a board.
func (gc *GhostboardController) DeleteFile(c *gin.Context) {
	if !gc.filesAvailable(c) {
		return
	}

	if err := gc.store.DeleteFile(ghostboardBoardName(c), strings.TrimSpace(c.Param("id")), ghostboardClientID(c)); err != nil {
		gc.writeFileError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// DeleteFiles removes every attachment of a board, which is what the Clear
// dialog asks for after its second choice.
func (gc *GhostboardController) DeleteFiles(c *gin.Context) {
	if !gc.filesAvailable(c) {
		return
	}

	if _, err := gc.store.DeleteFiles(ghostboardBoardName(c), ghostboardClientID(c)); err != nil {
		gc.writeFileError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// storeUpload parses the multipart body, streams its first part named "file"
// into the store and reports whether the request produced an attachment. Every
// failure is answered here, so the two upload surfaces stay identical.
func (gc *GhostboardController) storeUpload(c *gin.Context, board string) (ghostboard.File, bool) {
	if err := ghostboard.ValidateBoardName(board); err != nil {
		writeGhostboardError(c, err)
		return ghostboard.File{}, false
	}

	if !gc.filesAvailable(c) {
		return ghostboard.File{}, false
	}
	limits := gc.store.FileLimits()
	if c.ContentType() != "multipart/form-data" {
		rejectFile(c, http.StatusUnsupportedMediaType, gin.H{"error": "expected multipart/form-data"},
			fmt.Errorf("content type %q is not multipart/form-data", c.ContentType()))
		return ghostboard.File{}, false
	}

	// The body limit is the file limit plus the envelope: the store enforces
	// the file limit on the bytes it receives, and this one stops a body that
	// carries far more than a single file before it is stored.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limits.MaxFileBytes+ghostboardUploadOverhead)

	if err := http.NewResponseController(c.Writer).SetReadDeadline(time.Now().Add(limits.TransferTimeout)); err != nil {
		logger.WithComponent("ghostboard").Debugf("cannot set the upload deadline: %v", err)
	}

	reader, err := c.Request.MultipartReader()
	if err != nil {
		rejectFile(c, http.StatusBadRequest, gin.H{"error": "invalid multipart body"},
			fmt.Errorf("reading the multipart envelope: %w", err))
		return ghostboard.File{}, false
	}

	part, ok := firstFilePart(c, reader)
	if !ok {
		return ghostboard.File{}, false
	}
	if part.FileName() == "" {
		rejectFile(c, http.StatusBadRequest, gin.H{"error": "the file part has no file name"},
			errors.New("the file part has no file name"))
		return ghostboard.File{}, false
	}

	file, err := gc.store.WriteFile(c.Request.Context(), board, part.FileName(), ghostboardClientID(c), part)
	if closeErr := part.Close(); closeErr != nil {
		logger.WithComponent("ghostboard").Debugf("closing the upload part of board %q: %v", board, closeErr)
	}
	drainMultipart(reader)

	if err != nil {
		gc.writeFileError(c, err)
		return ghostboard.File{}, false
	}
	return file, true
}

// firstFilePart walks the parts of an upload until it finds the one named
// "file". The parts before it are drained, so a client that puts a field first
// still works.
func firstFilePart(c *gin.Context, reader *multipart.Reader) (*multipart.Part, bool) {
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			rejectFile(c, http.StatusBadRequest, gin.H{"error": "multipart body without a file part"},
				errors.New("multipart body without a file part"))
			return nil, false
		}
		if err != nil {
			// A body over the limit fails here, before a single byte of the
			// file reaches the store.
			writeUploadReadError(c, err)
			return nil, false
		}
		if part.FormName() == "file" {
			return part, true
		}
		if _, err := io.Copy(io.Discard, io.LimitReader(part, ghostboardDrainLimit)); err != nil {
			writeUploadReadError(c, err)
			return nil, false
		}
		if err := part.Close(); err != nil {
			logger.WithComponent("ghostboard").Debugf("closing an ignored multipart part: %v", err)
		}
	}
}

// drainMultipart reads what is left of an upload, bounded, so the client is not
// cut off while the response is written.
func drainMultipart(reader *multipart.Reader) {
	budget := int64(ghostboardDrainLimit)
	for budget > 0 {
		part, err := reader.NextPart()
		if err != nil {
			return
		}
		read, copyErr := io.Copy(io.Discard, io.LimitReader(part, budget))
		budget -= read
		if closeErr := part.Close(); closeErr != nil {
			logger.WithComponent("ghostboard").Debugf("closing an ignored multipart part: %v", closeErr)
		}
		if copyErr != nil {
			logger.WithComponent("ghostboard").Debugf("draining the rest of an upload: %v", copyErr)
			return
		}
	}
}

// writeUploadReadError maps a failure to read the multipart envelope.
func writeUploadReadError(c *gin.Context, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		rejectFile(c, http.StatusRequestEntityTooLarge, gin.H{
			"error":       ghostboard.ErrFileTooLarge.Error(),
			"limit":       "max_file_bytes",
			"limit_bytes": maxBytesErr.Limit - ghostboardUploadOverhead,
		}, fmt.Errorf("the upload body exceeds max_file_bytes: %w", err))
		return
	}
	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		rejectFile(c, http.StatusGatewayTimeout, gin.H{"error": ghostboard.ErrFileTransferTimeout.Error()}, err)
		return
	}
	rejectFile(c, http.StatusBadRequest, gin.H{"error": "invalid multipart body"}, fmt.Errorf("reading the multipart envelope: %w", err))
}

// writeFileError maps the attachment failures to stable statuses and bodies.
// The body names the limit that was hit, so the page and the CLI can say what
// to do instead of only reporting a number.
func (gc *GhostboardController) writeFileError(c *gin.Context, err error) {
	limits := gc.store.FileLimits()

	var maxBytesErr *http.MaxBytesError
	switch {
	case errors.Is(err, ghostboard.ErrFilesDisabled), errors.Is(err, ghostboard.ErrFileNotFound):
		rejectFile(c, http.StatusNotFound, gin.H{"error": "file not found"}, err)
	case errors.Is(err, ghostboard.ErrInvalidFileID):
		rejectFile(c, http.StatusBadRequest, gin.H{"error": "invalid file id"}, err)
	case errors.Is(err, ghostboard.ErrInvalidFileName):
		rejectFile(c, http.StatusBadRequest, gin.H{"error": "the file part has no usable file name"}, err)
	case errors.Is(err, ghostboard.ErrEmptyFile):
		rejectFile(c, http.StatusBadRequest, gin.H{"error": "the uploaded file is empty"}, err)
	case errors.Is(err, ghostboard.ErrFileTooLarge), errors.As(err, &maxBytesErr):
		rejectFile(c, http.StatusRequestEntityTooLarge, gin.H{
			"error":       ghostboard.ErrFileTooLarge.Error(),
			"limit":       "max_file_bytes",
			"limit_bytes": limits.MaxFileBytes,
		}, err)
	case errors.Is(err, ghostboard.ErrFileCountExceeded):
		rejectFile(c, http.StatusConflict, gin.H{
			"error":       ghostboard.ErrFileCountExceeded.Error(),
			"limit":       "max_file_count",
			"limit_value": limits.MaxFileCount,
		}, err)
	case errors.Is(err, ghostboard.ErrBoardFileQuotaExceeded):
		rejectFile(c, http.StatusRequestEntityTooLarge, gin.H{
			"error":       ghostboard.ErrBoardFileQuotaExceeded.Error(),
			"limit":       "max_board_file_bytes",
			"limit_bytes": limits.MaxBoardBytes,
		}, err)
	case errors.Is(err, ghostboard.ErrTotalFileQuotaExceeded):
		rejectFile(c, http.StatusRequestEntityTooLarge, gin.H{
			"error":       ghostboard.ErrTotalFileQuotaExceeded.Error(),
			"limit":       "max_total_file_bytes",
			"limit_bytes": limits.MaxTotalBytes,
		}, err)
	case errors.Is(err, ghostboard.ErrNoSpace):
		rejectFile(c, http.StatusInsufficientStorage, gin.H{"error": ghostboard.ErrNoSpace.Error()}, err)
	case errors.Is(err, os.ErrDeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
		rejectFile(c, http.StatusGatewayTimeout, gin.H{"error": ghostboard.ErrFileTransferTimeout.Error()}, err)
	case errors.Is(err, context.Canceled):
		// The client went away mid-transfer; there is nobody to answer.
		logger.WithComponent("ghostboard").Debugf("attachment request on %s cancelled by the client", fileRequestTarget(c))
		c.Abort()
	default:
		logger.WithComponent("ghostboard").Errorf("attachment request on %s failed: %v", fileRequestTarget(c), err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

// rejectFile answers a refused attachment request and records it at Warn, with
// the board, the file, the limit that was hit and the reason. The page only
// shows the sentence of the body to the client, so this line is what tells an
// operator that, and why, a limit was hit.
func rejectFile(c *gin.Context, status int, body gin.H, err error) {
	limit := ""
	if name, ok := body["limit"].(string); ok && name != "" {
		limit = fmt.Sprintf(" (limit %s)", name)
	}
	logger.WithComponent("ghostboard").Warnf("refused an attachment request on %s%s: %v", fileRequestTarget(c), limit, err)
	c.JSON(status, body)
}

// fileRequestTarget names the board and, when the request carried one, the file
// of an attachment request, for a log line.
func fileRequestTarget(c *gin.Context) string {
	board := ghostboardBoardName(c)
	if id := strings.TrimSpace(c.Param("id")); id != "" {
		return fmt.Sprintf("board %q, file %s", board, id)
	}
	return fmt.Sprintf("board %q", board)
}

// filenameLine makes a file name safe for one line of the plain listing: a
// tab or a newline in it would forge a field or a line.
func filenameLine(name string) string {
	return strings.NewReplacer("\t", " ", "\r", " ", "\n", " ").Replace(name)
}

// ghostboardContentDisposition builds the download header. The quoted fallback
// keeps old clients working; filename* carries the real name in UTF-8, which is
// how a non-ASCII name survives the header.
func ghostboardContentDisposition(name string) string {
	fallback := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		fallback = "file"
	}
	return fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", fallback, rfc5987(name))
}

// rfc5987 percent-encodes a value for the filename* parameter, keeping only the
// attr-char set the standard allows unescaped.
func rfc5987(value string) string {
	var encoded strings.Builder
	for i := 0; i < len(value); i++ {
		b := value[i]
		if isAttrChar(b) {
			encoded.WriteByte(b)
			continue
		}
		fmt.Fprintf(&encoded, "%%%02X", b)
	}
	return encoded.String()
}

// isAttrChar reports whether a byte may appear unescaped in an RFC 5987 value.
func isAttrChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	}
	switch b {
	case '!', '#', '$', '&', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}
