package ghostboard

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/asgambat/gospin/internal/logger"
)

// Errors returned by the attachment operations.
var (
	// ErrFilesDisabled marks a store without attachments configured.
	ErrFilesDisabled = errors.New("file attachments are disabled")
	// ErrFileNotFound marks an unknown attachment of a board.
	ErrFileNotFound = errors.New("file not found")
	// ErrInvalidFileID marks an id that cannot address an attachment.
	ErrInvalidFileID = errors.New("invalid file id")
	// ErrInvalidFileName marks a multipart part without a usable file name.
	ErrInvalidFileName = errors.New("invalid file name")
	// ErrEmptyFile marks an upload without a single byte, which the board does
	// not store: an attachment that carries nothing is a mistake.
	ErrEmptyFile = errors.New("the uploaded file is empty")
	// ErrFileTooLarge marks an upload over max_file_bytes.
	ErrFileTooLarge = errors.New("file exceeds the maximum allowed size")
	// ErrFileCountExceeded marks a board that already holds max_file_count
	// attachments.
	ErrFileCountExceeded = errors.New("too many files on this board")
	// ErrBoardFileQuotaExceeded marks a board over max_board_file_bytes.
	ErrBoardFileQuotaExceeded = errors.New("this board holds the maximum number of bytes of attachments")
	// ErrTotalFileQuotaExceeded marks an instance over max_total_file_bytes.
	ErrTotalFileQuotaExceeded = errors.New("the server holds the maximum number of bytes of attachments")
	// ErrNoSpace marks a disk that rejected the write.
	ErrNoSpace = errors.New("not enough storage")
	// ErrFileTransferTimeout marks a transfer that hit file_transfer_timeout_secs.
	ErrFileTransferTimeout = errors.New("transfer timeout")
)

const (
	// fileNameMaxRunes bounds the download label of an attachment. The label is
	// display and header material only: the file on disk is named after its id.
	fileNameMaxRunes = 200

	// fileHeadBytes is how much of an upload is sniffed for its content type.
	// It is the size http.DetectContentType inspects.
	fileHeadBytes = 512

	// fileTempPrefix marks the in-progress uploads inside a board directory.
	// Reconciliation deletes anything that still carries it at startup.
	fileTempPrefix = ".tmp-"

	// fileIDBytes is the length of a file id before encoding.
	fileIDBytes = 16
)

// fileIDPattern matches the opaque ids handed out for attachments: 128 bits in
// lowercase base32 without padding.
var fileIDPattern = regexp.MustCompile(`^[a-z2-7]{26}$`)

// FileConfig describes the attachments of the boards. Enabled false turns the
// whole feature off; Root is the directory holding one sub-directory per board.
type FileConfig struct {
	Root            string
	MaxFileBytes    int64
	MaxBoardBytes   int64
	MaxTotalBytes   int64
	MaxFileCount    int
	TTL             time.Duration
	TransferTimeout time.Duration
	Enabled         bool
}

// File is one attachment of a board. ID is the opaque handle used in URLs and
// as the name on disk; Name is the download label the uploader sent.
type File struct {
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
}

// validate rejects a file configuration that cannot work, naming the key that
// has to change.
func (c FileConfig) validate() error {
	if !c.Enabled {
		return nil
	}
	if c.Root == "" {
		return errors.New("ghostboard files directory is required when attachments are enabled")
	}
	if c.MaxFileBytes <= 0 {
		return errors.New("ghostboard max_file_bytes must be positive")
	}
	if c.MaxFileCount <= 0 {
		return errors.New("ghostboard max_file_count must be positive")
	}
	if c.MaxBoardBytes <= 0 {
		return errors.New("ghostboard max_board_file_bytes must be positive")
	}
	if c.MaxTotalBytes <= 0 {
		return errors.New("ghostboard max_total_file_bytes must be positive")
	}
	if c.MaxFileBytes > c.MaxBoardBytes || c.MaxBoardBytes > c.MaxTotalBytes {
		return errors.New("ghostboard file limits must not decrease: max_file_bytes <= max_board_file_bytes <= max_total_file_bytes")
	}
	if c.TTL <= 0 {
		return errors.New("ghostboard file_ttl_secs must be positive")
	}
	if c.TransferTimeout <= 0 {
		return errors.New("ghostboard file_transfer_timeout_secs must be positive")
	}
	return nil
}

// FileLimits returns the effective attachment limits, so a caller can turn a
// rejection into a message naming the limit that was hit.
func (s *Store) FileLimits() FileConfig {
	return s.cfg.Files
}

// FilesEnabled reports whether the store accepts attachments.
func (s *Store) FilesEnabled() bool {
	return s.cfg.Files.Enabled
}

// Files returns the attachments of a board, newest first.
func (s *Store) Files(board string) ([]File, error) {
	if err := ValidateBoardName(board); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.boards[board]
	if !ok {
		return []File{}, nil
	}
	return cloneFiles(state.board.Files), nil
}

// File returns one attachment of a board.
func (s *Store) File(board, id string) (File, error) {
	if err := ValidateBoardName(board); err != nil {
		return File{}, err
	}
	if !fileIDPattern.MatchString(id) {
		return File{}, ErrInvalidFileID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.boards[board]
	if !ok {
		return File{}, ErrFileNotFound
	}
	for _, file := range state.board.Files {
		if file.ID == id {
			return file, nil
		}
	}
	return File{}, ErrFileNotFound
}

// OpenFile returns the metadata and the open handle of an attachment. The
// caller closes the returned file. Symlinks are refused: an attachment is a
// regular file this store wrote, never a link somewhere else.
func (s *Store) OpenFile(board, id string) (File, *os.File, error) {
	file, err := s.File(board, id)
	if err != nil {
		return File{}, nil, err
	}

	path, err := s.filePath(board, id)
	if err != nil {
		return File{}, nil, err
	}

	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return File{}, nil, ErrFileNotFound
		}
		return File{}, nil, fmt.Errorf("stat attachment %s of board %q: %w", id, board, err)
	}
	if !info.Mode().IsRegular() {
		return File{}, nil, ErrFileNotFound
	}

	handle, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return File{}, nil, ErrFileNotFound
		}
		return File{}, nil, fmt.Errorf("open attachment %s of board %q: %w", id, board, err)
	}
	return file, handle, nil
}

// WriteFile streams r to disk as a new attachment of the board and commits it.
// The upload is written to a temporary file first, so a rejected transfer (a
// quota, a client that disappeared, a full disk) leaves nothing behind.
//
// Quotas are checked after the bytes are on disk and before the commit, under
// the store lock: the temporary file is the reservation, and two concurrent
// uploads cannot both pass a board that is one byte short of its limit.
func (s *Store) WriteFile(ctx context.Context, board, name, clientID string, r io.Reader) (File, error) {
	if err := ValidateBoardName(board); err != nil {
		return File{}, err
	}
	if !s.cfg.Files.Enabled {
		return File{}, ErrFilesDisabled
	}

	label, err := SanitizeFileName(name)
	if err != nil {
		return File{}, err
	}

	dir := filepath.Join(s.cfg.Files.Root, board)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return File{}, fileWriteError(fmt.Errorf("create board directory %s: %w", dir, err))
	}

	temp, err := os.CreateTemp(dir, fileTempPrefix+"*")
	if err != nil {
		return File{}, fileWriteError(fmt.Errorf("create upload file in %s: %w", dir, err))
	}
	tempPath := temp.Name()

	committed := false
	defer func() {
		if err := temp.Close(); err != nil {
			logger.WithComponent("ghostboard").Debugf("closing upload file %s: %v", tempPath, err)
		}
		if !committed {
			if err := os.Remove(tempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				logger.WithComponent("ghostboard").Warnf("cannot remove rejected upload %s: %v", tempPath, err)
			}
		}
	}()

	head := make([]byte, fileHeadBytes)
	read, err := io.ReadFull(r, head)
	switch {
	case err == nil, errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
	case ctx.Err() != nil:
		return File{}, ctx.Err()
	default:
		return File{}, fileWriteError(fmt.Errorf("read the upload for board %q: %w", board, err))
	}
	head = head[:read]

	if int64(read) > s.cfg.Files.MaxFileBytes {
		return File{}, ErrFileTooLarge
	}
	if read > 0 {
		if _, err := temp.Write(head); err != nil {
			return File{}, fileWriteError(fmt.Errorf("write the upload for board %q: %w", board, err))
		}
	}

	// One byte more than the limit is enough to tell "too large" from "exactly
	// at the limit", without reading an unbounded stream into memory.
	rest, err := io.Copy(temp, io.LimitReader(r, s.cfg.Files.MaxFileBytes-int64(read)+1))
	if err != nil {
		if ctx.Err() != nil {
			return File{}, ctx.Err()
		}
		return File{}, fileWriteError(fmt.Errorf("stream the upload for board %q: %w", board, err))
	}

	size := int64(read) + rest
	switch {
	case size > s.cfg.Files.MaxFileBytes:
		return File{}, ErrFileTooLarge
	case size == 0:
		return File{}, ErrEmptyFile
	}

	contentType := http.DetectContentType(head)
	if detected, _, err := mime.ParseMediaType(contentType); err == nil {
		contentType = detected
	}

	now := time.Now().UTC()
	file := File{
		CreatedAt:   now,
		ExpiresAt:   now.Add(s.cfg.Files.TTL),
		Name:        label,
		ContentType: contentType,
		Size:        size,
	}

	id, err := newFileID()
	if err != nil {
		return File{}, err
	}
	file.ID = id

	finalPath := filepath.Join(dir, id)

	s.mu.Lock()
	// Announce the expired files first: a client whose list is open must lose
	// them even when the upload that triggered the sweep is then rejected.
	for _, changed := range s.sweepExpiredLocked(now) {
		s.broadcastLocked(changed, "")
	}
	state := s.boardLocked(board)
	if err := s.checkFileQuotaLocked(state, size); err != nil {
		s.mu.Unlock()
		return File{}, err
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		s.mu.Unlock()
		return File{}, fileWriteError(fmt.Errorf("commit the upload for board %q: %w", board, err))
	}
	committed = true

	state.board.Files = append([]File{file}, state.board.Files...)
	s.dirty = true
	s.scheduleIdleClearLocked(board, state)
	s.broadcastLocked(state, clientID)
	s.mu.Unlock()

	logger.WithComponent("ghostboard").Infof("stored attachment %s (%d byte(s)) on board %q", file.ID, size, board)
	return file, nil
}

// DeleteFile removes one attachment from a board, from memory and from disk.
func (s *Store) DeleteFile(board, id, clientID string) error {
	if err := ValidateBoardName(board); err != nil {
		return err
	}
	if !fileIDPattern.MatchString(id) {
		return ErrInvalidFileID
	}

	s.mu.Lock()
	state, ok := s.boards[board]
	if !ok {
		s.mu.Unlock()
		return ErrFileNotFound
	}

	index := -1
	for i, file := range state.board.Files {
		if file.ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		s.mu.Unlock()
		return ErrFileNotFound
	}

	state.board.Files = append(state.board.Files[:index:index], state.board.Files[index+1:]...)
	s.dirty = true
	s.scheduleIdleClearLocked(board, state)
	s.broadcastLocked(state, clientID)
	s.mu.Unlock()

	// The metadata is gone either way: an attachment that cannot be removed
	// from disk must not be handed out as if it were still there.
	s.removeFileFromDisk(board, id)
	logger.WithComponent("ghostboard").Infof("deleted attachment %s from board %q", id, board)
	return nil
}

// DeleteFiles removes every attachment of a board and reports how many were
// dropped. The board text is left alone: the page asks for the two separately.
func (s *Store) DeleteFiles(board, clientID string) (int, error) {
	if err := ValidateBoardName(board); err != nil {
		return 0, err
	}

	s.mu.Lock()
	state, ok := s.boards[board]
	if !ok || len(state.board.Files) == 0 {
		s.mu.Unlock()
		return 0, nil
	}

	files := state.board.Files
	state.board.Files = nil
	s.dirty = true
	s.scheduleIdleClearLocked(board, state)
	s.broadcastLocked(state, clientID)
	s.mu.Unlock()

	for _, file := range files {
		s.removeFileFromDisk(board, file.ID)
	}

	logger.WithComponent("ghostboard").Infof("deleted %d attachment(s) from board %q", len(files), board)
	return len(files), nil
}

// SweepFiles drops the attachments whose file_ttl has elapsed. It runs at
// startup (through ReconcileFiles), on every persistence tick and before a
// quota check, so an expired file frees its bytes as soon as anything looks.
func (s *Store) SweepFiles() error {
	if !s.cfg.Files.Enabled {
		return nil
	}

	s.mu.Lock()
	changed := s.sweepExpiredLocked(time.Now().UTC())
	for _, state := range changed {
		s.broadcastLocked(state, "")
	}
	s.mu.Unlock()
	return nil
}

// ReconcileFiles makes the disk and the persisted metadata agree at startup:
// files without metadata, metadata without a file, unknown board directories
// and leftovers of interrupted uploads all go away. It never follows a symlink.
func (s *Store) ReconcileFiles() error {
	if !s.cfg.Files.Enabled {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.cfg.Files.Root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read ghostboard files directory %s: %w", s.cfg.Files.Root, err)
	}

	for _, entry := range entries {
		dir := filepath.Join(s.cfg.Files.Root, entry.Name())
		board := entry.Name()

		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			s.removeOrphan(dir, "not a board directory")
			continue
		}
		if err := ValidateBoardName(board); err != nil {
			s.removeOrphan(dir, "invalid board name")
			continue
		}
		state, ok := s.boards[board]
		if !ok {
			s.removeOrphan(dir, "unknown board")
			continue
		}
		s.reconcileBoardDir(board, dir, state)
	}

	// A board directory that never existed means every one of its attachments
	// is missing: drop the metadata that points at nothing.
	for _, state := range s.boards {
		if s.dropMissingFilesLocked(state) {
			s.dirty = true
		}
	}

	return nil
}

// checkFileQuotaLocked reports whether one more file of size bytes fits in the
// board and in the instance.
func (s *Store) checkFileQuotaLocked(state *boardState, size int64) error {
	if len(state.board.Files) >= s.cfg.Files.MaxFileCount {
		return ErrFileCountExceeded
	}
	if fileBytes(state.board.Files)+size > s.cfg.Files.MaxBoardBytes {
		return ErrBoardFileQuotaExceeded
	}
	if s.totalFileBytesLocked()+size > s.cfg.Files.MaxTotalBytes {
		return ErrTotalFileQuotaExceeded
	}
	return nil
}

// totalFileBytesLocked is the size of every attachment the store holds.
func (s *Store) totalFileBytesLocked() int64 {
	var total int64
	for _, state := range s.boards {
		total += fileBytes(state.board.Files)
	}
	return total
}

// sweepExpiredLocked removes the attachments past their expires_at and returns
// the boards that changed, so the caller can announce them.
func (s *Store) sweepExpiredLocked(now time.Time) []*boardState {
	var changed []*boardState

	for _, state := range s.boards {
		if len(state.board.Files) == 0 {
			continue
		}

		kept := make([]File, 0, len(state.board.Files))
		for _, file := range state.board.Files {
			if file.ExpiresAt.After(now) {
				kept = append(kept, file)
				continue
			}
			logger.WithComponent("ghostboard").Debugf("attachment %s of board %q expired", file.ID, state.board.Name)
			s.removeFileFromDisk(state.board.Name, file.ID)
		}
		if len(kept) == len(state.board.Files) {
			continue
		}

		state.board.Files = kept
		s.dirty = true
		changed = append(changed, state)
	}

	return changed
}

// removeBoardFilesLocked deletes every attachment of a board from disk and
// from its metadata.
func (s *Store) removeBoardFilesLocked(state *boardState) {
	for _, file := range state.board.Files {
		s.removeFileFromDisk(state.board.Name, file.ID)
	}
	state.board.Files = nil
}

// reconcileBoardDir aligns one board directory with the metadata: leftovers of
// interrupted uploads, entries without metadata, directories and symlinks all
// go away.
func (s *Store) reconcileBoardDir(board, dir string, state *boardState) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		logger.WithComponent("ghostboard").Warnf("cannot read the files of board %q: %v", board, err)
		return
	}

	kept := make(map[string]struct{}, len(state.board.Files))
	for _, file := range state.board.Files {
		kept[file.ID] = struct{}{}
	}

	for _, entry := range entries {
		name := entry.Name()
		full := filepath.Join(dir, name)

		switch {
		case entry.Type()&os.ModeSymlink != 0:
			s.removeOrphan(full, "symlink in board "+board)
		case entry.IsDir():
			s.removeOrphan(full, "directory in board "+board)
		case strings.HasPrefix(name, fileTempPrefix):
			s.removeOrphan(full, "interrupted upload in board "+board)
		default:
			if _, ok := kept[name]; !ok {
				s.removeOrphan(full, "file without metadata in board "+board)
			}
		}
	}
}

// dropMissingFilesLocked drops the metadata of a board whose file is gone and
// reports whether anything changed.
func (s *Store) dropMissingFilesLocked(state *boardState) bool {
	if len(state.board.Files) == 0 {
		return false
	}

	kept := make([]File, 0, len(state.board.Files))
	for _, file := range state.board.Files {
		path, err := s.filePath(state.board.Name, file.ID)
		if err != nil {
			continue
		}
		info, statErr := os.Lstat(path)
		if statErr == nil && info.Mode().IsRegular() {
			kept = append(kept, file)
			continue
		}

		logger.WithComponent("ghostboard").Warnf("dropping metadata of missing attachment %s on board %q", file.ID, state.board.Name)
	}

	if len(kept) == len(state.board.Files) {
		return false
	}
	state.board.Files = kept
	return true
}

// removeOrphan deletes a path that does not belong to the store and says so.
func (s *Store) removeOrphan(path, reason string) {
	logger.WithComponent("ghostboard").Warnf("removing %s: %s", path, reason)
	if err := os.RemoveAll(path); err != nil {
		logger.WithComponent("ghostboard").Errorf("cannot remove %s: %v", path, err)
	}
}

// removeFileFromDisk deletes the bytes of an attachment, reporting a failure
// that the metadata change already made harmless.
func (s *Store) removeFileFromDisk(board, id string) {
	path, err := s.filePath(board, id)
	if err != nil {
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		logger.WithComponent("ghostboard").Warnf("cannot remove attachment %s of board %q: %v", id, board, err)
	}
}

// filePath builds the on-disk path of an attachment, refusing anything that
// could leave the board directory.
func (s *Store) filePath(board, id string) (string, error) {
	if !fileIDPattern.MatchString(id) {
		return "", ErrInvalidFileID
	}
	dir := filepath.Join(s.cfg.Files.Root, board)
	full := filepath.Join(dir, id)
	if filepath.Dir(full) != dir {
		return "", ErrInvalidFileID
	}
	return full, nil
}

// broadcastLocked hands the current state of a board to its subscribers.
func (s *Store) broadcastLocked(state *boardState, clientID string) {
	event := Event{Board: cloneBoard(state.board), ClientID: clientID}
	for sub := range state.subs {
		select {
		case sub.events <- event:
		default:
			logger.WithComponent("ghostboard").Debugf("dropping event for slow subscriber on board %q", state.board.Name)
		}
	}
}

// SanitizeFileName turns the name an uploader sent into the download label of
// an attachment. It keeps the readable part, drops the parts that would end up
// in a header or a page (control characters, path separators) and bounds the
// length. An empty or unusable name is rejected.
func SanitizeFileName(name string) (string, error) {
	// A client may send a Windows path; only its last element is a name.
	trimmed := path.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	if trimmed == "" || trimmed == "." || trimmed == ".." || trimmed == "/" {
		return "", ErrInvalidFileName
	}

	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, trimmed)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return "", ErrInvalidFileName
	}

	if utf8.RuneCountInString(cleaned) > fileNameMaxRunes {
		cleaned = string([]rune(cleaned)[:fileNameMaxRunes])
	}
	return cleaned, nil
}

// newFileID returns 128 random bits as lowercase base32 without padding, so an
// id is URL-safe, filesystem-safe and unguessable.
func newFileID() (string, error) {
	var raw [fileIDBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate a file id: %w", err)
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:])
	return strings.ToLower(encoded), nil
}

// fileWriteError maps a disk failure to the error the API answers with: a full
// disk is a state of the server, not a bad request.
func fileWriteError(err error) error {
	if errors.Is(err, syscall.ENOSPC) {
		return ErrNoSpace
	}
	return err
}

// fileBytes is the sum of the sizes of a board's attachments.
func fileBytes(files []File) int64 {
	var total int64
	for _, file := range files {
		total += file.Size
	}
	return total
}

// cloneBoard copies a board, so a caller can neither mutate the store by
// touching a slice nor see the empty nil slice as a missing field.
func cloneBoard(board Board) Board {
	board.Files = cloneFiles(board.Files)
	return board
}

// normalizedBoard makes the attachments of a board an empty slice instead of
// nil, so the JSON of a board without files carries "files": [] rather than
// null.
func normalizedBoard(board Board) Board {
	if board.Files == nil {
		board.Files = []File{}
	}
	return board
}

// cloneFiles copies the attachment list.
func cloneFiles(files []File) []File {
	if len(files) == 0 {
		return []File{}
	}
	return append([]File{}, files...)
}

// validFiles keeps the attachments a persisted document can be trusted with:
// a valid id and a positive size. Anything else is dropped with a warning, so
// one bad entry cannot stop the server from starting.
func validFiles(board string, files []File) ([]File, error) {
	if len(files) == 0 {
		return nil, nil
	}

	kept := make([]File, 0, len(files))
	for _, file := range files {
		if !fileIDPattern.MatchString(file.ID) {
			logger.WithComponent("ghostboard").Warnf("dropping attachment with an invalid id on board %q", board)
			continue
		}
		if file.Size <= 0 {
			logger.WithComponent("ghostboard").Warnf("dropping attachment %s of board %q with a non-positive size", file.ID, board)
			continue
		}
		if _, err := SanitizeFileName(file.Name); err != nil {
			file.Name = "file"
		}
		kept = append(kept, file)
	}

	sort.SliceStable(kept, func(i, j int) bool { return kept[i].CreatedAt.After(kept[j].CreatedAt) })
	return kept, nil
}
