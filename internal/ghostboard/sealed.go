package ghostboard

import (
	"errors"
	"fmt"
	"time"

	"github.com/asgambat/gospin/internal/logger"
	"github.com/asgambat/gospin/internal/sealedcrypto"
)

// Mode says how a board stores its text: a plain board holds the text, a sealed
// board holds one opaque envelope that the client encrypts and decrypts.
type Mode string

const (
	// ModePlain is a board whose text is readable by the server.
	ModePlain Mode = "plain"
	// ModeSealed is a board whose text is an AES-256-GCM envelope.
	ModeSealed Mode = "sealed"

	// DefaultSealedTombstone is how long a burned board keeps answering as
	// burned when the store is built without a configured duration.
	DefaultSealedTombstone = 24 * time.Hour
)

// Errors returned by the sealed board operations. They are exported sentinels so
// the HTTP layer can map each situation to a stable status code.
var (
	// ErrBoardNotSealed marks an Open of a board that is not a burn board.
	ErrBoardNotSealed = errors.New("board is not a burn board")
	// ErrBoardBurned marks a name whose board was burned and whose tombstone has
	// not expired yet.
	ErrBoardBurned = errors.New("board was burned and no longer exists")
	// ErrBurnRequiresOpen marks a plain read or an event stream of a burn board
	// that was not opened: both would consume the note before a reader asked
	// for it.
	ErrBurnRequiresOpen = errors.New("burn board must be opened explicitly")
	// ErrPlaintextWriteToSealed marks a plaintext write to a sealed board, which
	// would silently turn "the server cannot read this" into a plain board.
	ErrPlaintextWriteToSealed = errors.New("board is sealed: write an envelope")
	// ErrSealedOverPlainText marks a sealed write over a plain board that already
	// holds text: converting would require reading the plaintext.
	ErrSealedOverPlainText = errors.New("board already holds plaintext and cannot be sealed")
	// ErrSealedBoardFiles marks an attachment operation on a sealed board.
	ErrSealedBoardFiles = errors.New("sealed boards cannot carry attachments")
	// ErrSealedDisabled marks a sealed operation while ghostboard.sealed_enabled
	// is false.
	ErrSealedDisabled = errors.New("sealed boards are disabled")
	// ErrBoardNotFound marks an unknown board for Open, including a name whose
	// tombstone already expired.
	ErrBoardNotFound = errors.New("board not found")
)

// SealedWrite is one write of a sealed board. Burn is read only when the write
// creates the board: the mode and the burn flag are fixed at creation.
type SealedWrite struct {
	Name     string
	Envelope string
	ClientID string
	Burn     bool
}

// tombstone is a burned name, persisted next to the boards so a restart cannot
// resurrect a note.
type tombstone struct {
	BurnedAt time.Time `json:"burned_at"`
	Name     string    `json:"name"`
}

// UpdateSealed stores an envelope for a board, creating it as sealed when it does
// not exist yet. A plaintext write to a sealed board and a sealed write over a
// plain board that holds text are both refused: the mode never changes after
// creation, because converting a plain board would have to read its plaintext.
func (s *Store) UpdateSealed(w SealedWrite) (Board, error) {
	if err := ValidateBoardName(w.Name); err != nil {
		return Board{}, err
	}
	if !s.cfg.SealedEnabled {
		return Board{}, ErrSealedDisabled
	}
	if len(w.Envelope) > s.cfg.MaxTextBytes {
		return Board{}, ErrTextTooLarge
	}
	if _, err := sealedcrypto.ParseEnvelope(w.Envelope); err != nil {
		return Board{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, burned := s.burnedAtLocked(w.Name); burned {
		return Board{}, ErrBoardBurned
	}

	state := s.boardLocked(w.Name)
	switch {
	case state.board.Mode == ModeSealed:
		// Burn is set when the board is created and cannot be changed later;
		// a client asking otherwise is ignored, since honouring it would
		// either burn a note nobody asked to burn or keep one alive.
		if w.Burn != state.board.Burn {
			logger.WithComponent("ghostboard").Warnf("board %q is sealed with burn=%t: ignoring burn=%t on a later write", w.Name, state.board.Burn, w.Burn)
		}
		if state.board.Text == w.Envelope {
			s.scheduleIdleClearLocked(w.Name, state)
			return cloneBoard(state.board), nil
		}
	case state.board.Text != "":
		return Board{}, ErrSealedOverPlainText
	default:
		state.board.Mode = ModeSealed
		state.board.Burn = w.Burn
	}

	state.board.Text = w.Envelope
	state.board.Mode = ModeSealed
	state.board.Revision++
	state.board.UpdatedAt = time.Now().UTC()
	s.dirty = true

	s.broadcastLocked(state, w.ClientID)
	s.scheduleIdleClearLocked(w.Name, state)

	return cloneBoard(state.board), nil
}

// Open hands out the envelope of a burn board exactly once. The board is removed
// and a tombstone is recorded before the caller sees the text, and the tombstone
// is flushed to disk before the envelope leaves the store, so a second reader, a
// restart or a crash cannot resurrect the note.
//
// A failed flush reveals nothing and is retryable: the board is put back as it
// was, so handing out a note that is still on disk would break the promise the
// caller made to the writer. Opening stays available when sealed boards are
// disabled, so turning the feature off never strands a note somebody can still read.
func (s *Store) Open(name string) (Board, error) {
	if err := ValidateBoardName(name); err != nil {
		return Board{}, err
	}

	s.mu.Lock()

	if _, burned := s.burnedAtLocked(name); burned {
		s.mu.Unlock()
		return Board{}, ErrBoardBurned
	}

	state, ok := s.boards[name]
	if !ok {
		s.mu.Unlock()
		return Board{}, ErrBoardNotFound
	}
	if state.board.Mode != ModeSealed || !state.board.Burn {
		s.mu.Unlock()
		return Board{}, ErrBoardNotSealed
	}
	if state.board.Text == "" {
		// Nothing to destroy and nothing to burn: a burn board that was never
		// written stays an unknown board instead of reserving its name.
		s.mu.Unlock()
		return Board{}, ErrBoardNotFound
	}

	envelope := state.board.Text
	burnedAt := time.Now().UTC()
	// The revision the burn started from: a write that lands while the flush
	// runs must not be overwritten by the failed-flush restore.
	burnedRevision := state.board.Revision

	opened := cloneBoard(state.board)
	opened.Revision++
	opened.UpdatedAt = burnedAt

	state.clearSeq++
	if state.clearTimer != nil {
		state.clearTimer.Stop()
		state.clearTimer = nil
	}
	state.board.Text = ""
	s.tombstones[name] = burnedAt
	s.dirty = true
	s.mu.Unlock()

	// Flush outside the lock: it takes the lock itself, and a burn that is not
	// on disk must not be answered with the note.
	if err := s.Flush(); err != nil {
		s.mu.Lock()
		s.restoreBurnAfterFailedFlushLocked(name, state, envelope, burnedRevision)
		s.mu.Unlock()
		return Board{}, fmt.Errorf("persisting the burn of board %q: %w", name, err)
	}

	s.mu.Lock()
	delete(s.boards, name)
	s.mu.Unlock()

	logger.WithComponent("ghostboard").Infof("burned sealed board %q on its first open", name)
	return opened, nil
}

// burnedAtLocked reports whether name is a live tombstone, dropping an expired
// one on the way. An expired tombstone frees the name: it answers as an unknown
// board from then on, so a stale link cannot reveal that a note ever existed.
func (s *Store) burnedAtLocked(name string) (time.Time, bool) {
	burnedAt, ok := s.tombstones[name]
	if !ok {
		return time.Time{}, false
	}
	if s.tombstoneExpired(burnedAt) {
		delete(s.tombstones, name)
		s.dirty = true
		logger.WithComponent("ghostboard").Debugf("tombstone of board %q expired after %s", name, s.cfg.SealedTombstone)
		return time.Time{}, false
	}
	return burnedAt, true
}

// tombstoneExpired reports whether a burn is old enough to stop answering as
// burned.
func (s *Store) tombstoneExpired(burnedAt time.Time) bool {
	return time.Since(burnedAt) >= s.cfg.SealedTombstone
}

// burnLocked destroys the envelope of an unopened burn board and records the
// tombstone. It is the idle-clear path of a burn board: the note is gone, and the
// name answers as burned for the tombstone window.
func (s *Store) burnLocked(name string, state *boardState, at time.Time) {
	state.board.Text = ""
	state.board.Revision++
	state.board.UpdatedAt = at
	s.tombstones[name] = at
	s.dirty = true
}

// sealedRefusalLocked returns ErrSealedBoardFiles when the board is sealed. Every
// attachment operation goes through it, because a board that carries a readable
// file is not zero-knowledge.
func (s *Store) sealedRefusalLocked(name string) error {
	if state, ok := s.boards[name]; ok && state.board.Mode == ModeSealed {
		return ErrSealedBoardFiles
	}
	return nil
}

// sealedRefusal is sealedRefusalLocked for a caller that does not hold the lock
// yet, such as an upload that would otherwise create a directory first.
func (s *Store) sealedRefusal(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sealedRefusalLocked(name)
}

// restoreBurnAfterFailedFlushLocked puts a burn board back after a flush that
// could not record it, so handing out the envelope later would break the promise
// the writer made. It only restores a board no write took control of in the
// meantime, matching the revision the burn started from; otherwise it leaves the
// tombstone and the emptied board for the persist loop to retry and for the
// tombstone window to expire, and reports what happened.
func (s *Store) restoreBurnAfterFailedFlushLocked(name string, state *boardState, envelope string, burnedRevision uint64) {
	restorable := state.board.Text == "" && state.board.Revision == burnedRevision
	if restorable {
		state.board.Text = envelope
		delete(s.tombstones, name)
		s.scheduleIdleClearLocked(name, state)
		logger.WithComponent("ghostboard").Debugf("restored burn board %q after a failed flush; the burn is retryable", name)
		return
	}

	logger.WithComponent("ghostboard").Warnf("board %q changed during a failed burn flush: keeping the tombstone and dropping the board text for the persist loop to retry", name)
}
