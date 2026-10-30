// Package ghostboard implements the multi-board shared text store behind the
// /ui/ghostboard page and its REST/SSE API.
//
// A board is addressed by name and holds the full text plus a revision counter.
// Subscribers (one per open event stream) receive every change so connected
// browsers stay in sync. Boards are kept in memory, optionally mirrored to a
// dedicated JSON file. The text is dropped once the configured clear delay has
// elapsed since the last write or the last subscriber leaving, whichever comes
// last, so a board written only by a script is cleared too.
package ghostboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/asgambat/gospin/internal/logger"
)

const (
	// DefaultBoard is the board served when the URL does not name one.
	DefaultBoard = "default"

	// subscriberBuffer is the number of changes buffered per open event stream.
	// A slow subscriber that fills the buffer skips the intermediate events; the
	// next delivered event always carries the full text, so it converges.
	subscriberBuffer = 16

	// filePerm and dirPerm are the permissions used for the persistence file and
	// its directory.
	filePerm = 0o600
	dirPerm  = 0o755
)

// Errors returned by the store.
var (
	ErrInvalidBoardName = errors.New("invalid board name")
	ErrTextTooLarge     = errors.New("text exceeds the maximum allowed size")
)

// boardNamePattern keeps board names URL-safe and filesystem-friendly.
var boardNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Board is a single shared text board with its attachments.
type Board struct {
	UpdatedAt time.Time `json:"updated_at"`
	Name      string    `json:"name"`
	Text      string    `json:"text"`
	Files     []File    `json:"files"`
	Revision  uint64    `json:"revision"`
}

// Event is one change broadcast to subscribers. Board fields are inlined and
// ClientID identifies the writer so its own browser can ignore the echo.
type Event struct {
	ClientID string `json:"client_id,omitempty"`
	Board
}

// Config holds the store settings. All fields are required except FilePath,
// which enables persistence when set, and Files, which disables attachments
// when Enabled is false.
type Config struct {
	FilePath        string
	Files           FileConfig
	ClearDelay      time.Duration
	MaxTextBytes    int
	PersistInterval time.Duration
}

// Store keeps every board in memory and fans changes out to subscribers.
type Store struct {
	boards    map[string]*boardState
	cfg       Config
	mu        sync.Mutex
	persistMu sync.Mutex
	dirty     bool
}

type boardState struct {
	subs       map[*Subscription]struct{}
	clearTimer *time.Timer
	board      Board
	// clearSeq is the generation of the pending idle clear. It sits after the
	// pointer fields so the GC pointer prefix stays as small as possible.
	clearSeq uint64
}

// Subscription is one open event stream for a board. Close must be called when
// the stream ends so the board can be cleared once it stays idle.
type Subscription struct {
	store   *Store
	events  chan Event
	board   string
	closeMu sync.Once
}

// Events returns the channel receiving board changes. It is closed when the
// subscription is closed.
func (s *Subscription) Events() <-chan Event {
	return s.events
}

// Close removes the subscription. It is safe to call more than once.
func (s *Subscription) Close() {
	s.closeMu.Do(func() {
		s.store.unsubscribe(s)
	})
}

// New creates a store, loading the persistence file when one is configured.
func New(cfg Config) (*Store, error) {
	if cfg.ClearDelay <= 0 {
		return nil, errors.New("ghostboard clear delay must be positive")
	}
	if cfg.MaxTextBytes <= 0 {
		return nil, errors.New("ghostboard max text size must be positive")
	}
	if cfg.PersistInterval <= 0 {
		return nil, errors.New("ghostboard persist interval must be positive")
	}
	if err := cfg.Files.validate(); err != nil {
		return nil, err
	}

	store := &Store{
		cfg:    cfg,
		boards: make(map[string]*boardState),
	}

	if cfg.FilePath != "" {
		if dir := filepath.Dir(cfg.FilePath); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, dirPerm); err != nil {
				return nil, fmt.Errorf("create ghostboard directory %s: %w", dir, err)
			}
		}
		if err := store.load(); err != nil {
			return nil, err
		}
	}

	if cfg.Files.Enabled {
		if err := os.MkdirAll(cfg.Files.Root, dirPerm); err != nil {
			return nil, fmt.Errorf("create ghostboard files directory %s: %w", cfg.Files.Root, err)
		}
		if err := store.ReconcileFiles(); err != nil {
			return nil, err
		}
	}

	return store, nil
}

// ValidateBoardName reports whether name can address a board.
func ValidateBoardName(name string) error {
	if !boardNamePattern.MatchString(name) {
		return ErrInvalidBoardName
	}
	return nil
}

// Get returns the current state of a board. Unknown boards are returned as
// empty boards with revision zero.
func (s *Store) Get(name string) (Board, error) {
	if err := ValidateBoardName(name); err != nil {
		return Board{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if state, ok := s.boards[name]; ok {
		return cloneBoard(state.board), nil
	}
	return normalizedBoard(Board{Name: name}), nil
}

// Update stores a new text for the board and broadcasts it to every subscriber.
// Updating a board with its current text is a no-op so typing pauses do not
// bump the revision. Accepted writes count as activity: they re-arm the idle
// clear, so a board written by a script alone is cleared after clear_delay too.
func (s *Store) Update(name, text, clientID string) (Board, error) {
	if err := ValidateBoardName(name); err != nil {
		return Board{}, err
	}
	if len(text) > s.cfg.MaxTextBytes {
		return Board{}, ErrTextTooLarge
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.boardLocked(name)
	if state.board.Text != text {
		state.board.Text = text
		state.board.Revision++
		state.board.UpdatedAt = time.Now().UTC()
		s.dirty = true

		event := Event{Board: state.board, ClientID: clientID}
		for sub := range state.subs {
			select {
			case sub.events <- event:
			default:
				logger.WithComponent("ghostboard").Debugf("dropping event for slow subscriber on board %q", name)
			}
		}
	}

	s.scheduleIdleClearLocked(name, state)

	return cloneBoard(state.board), nil
}

// Subscribe registers an event stream for the board and returns the current
// state to send as the initial snapshot. A pending clear is cancelled.
func (s *Store) Subscribe(name string) (*Subscription, Board, error) {
	if err := ValidateBoardName(name); err != nil {
		return nil, Board{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.boardLocked(name)
	if state.clearTimer != nil {
		state.clearTimer.Stop()
		state.clearTimer = nil
	}

	sub := &Subscription{
		store:  s,
		board:  name,
		events: make(chan Event, subscriberBuffer),
	}
	state.subs[sub] = struct{}{}

	return sub, cloneBoard(state.board), nil
}

// StartPersistence periodically writes dirty boards to disk until ctx is
// cancelled, flushing one last time on shutdown. It is a no-op without a file.
func (s *Store) StartPersistence(ctx context.Context) {
	if s.cfg.FilePath == "" {
		return
	}

	go func() {
		ticker := time.NewTicker(s.cfg.PersistInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				if err := s.Flush(); err != nil {
					logger.WithComponent("ghostboard").Errorf("final ghostboard flush failed: %v", err)
				}
				return
			case <-ticker.C:
				if err := s.SweepFiles(); err != nil {
					logger.WithComponent("ghostboard").Errorf("ghostboard file sweep failed: %v", err)
				}
				if err := s.Flush(); err != nil {
					logger.WithComponent("ghostboard").Errorf("ghostboard flush failed: %v", err)
				}
			}
		}
	}()
}

// Flush writes the boards to the persistence file when it is enabled and the
// store has pending changes. Empty boards are not persisted.
func (s *Store) Flush() error {
	if s.cfg.FilePath == "" {
		return nil
	}

	s.persistMu.Lock()
	defer s.persistMu.Unlock()

	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return nil
	}
	doc := fileDocument{Boards: s.persistedBoardsLocked()}
	s.dirty = false
	s.mu.Unlock()

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		s.markDirty()
		return fmt.Errorf("marshal ghostboard file: %w", err)
	}

	tmpPath := s.cfg.FilePath + ".tmp"
	if err := os.WriteFile(tmpPath, append(data, '\n'), filePerm); err != nil {
		s.markDirty()
		return fmt.Errorf("write ghostboard file: %w", err)
	}
	if err := os.Rename(tmpPath, s.cfg.FilePath); err != nil {
		s.markDirty()
		return fmt.Errorf("replace ghostboard file: %w", err)
	}

	logger.WithComponent("ghostboard").Debugf("persisted %d board(s) to %s", len(doc.Boards), s.cfg.FilePath)
	return nil
}

// MaxTextBytes returns the configured limit for a board text.
func (s *Store) MaxTextBytes() int {
	return s.cfg.MaxTextBytes
}

type fileDocument struct {
	Boards []Board `json:"boards"`
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.cfg.FilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read ghostboard file: %w", err)
	}

	var doc fileDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("decode ghostboard file %s: %w", s.cfg.FilePath, err)
	}

	for _, board := range doc.Boards {
		if err := ValidateBoardName(board.Name); err != nil {
			return fmt.Errorf("ghostboard file contains an invalid board name %q: %w", board.Name, err)
		}
		if len(board.Text) > s.cfg.MaxTextBytes {
			return fmt.Errorf("board %q in ghostboard file exceeds max_text_bytes", board.Name)
		}
		if board.Files, err = validFiles(board.Name, board.Files); err != nil {
			return err
		}
		s.boards[board.Name] = &boardState{
			board: normalizedBoard(board),
			subs:  make(map[*Subscription]struct{}),
		}
	}

	logger.WithComponent("ghostboard").Infof("loaded %d board(s) from %s", len(s.boards), s.cfg.FilePath)
	return nil
}

func (s *Store) boardLocked(name string) *boardState {
	if state, ok := s.boards[name]; ok {
		return state
	}
	state := &boardState{
		board: Board{Name: name},
		subs:  make(map[*Subscription]struct{}),
	}
	s.boards[name] = state
	return state
}

func (s *Store) unsubscribe(sub *Subscription) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.boards[sub.board]
	if ok {
		delete(state.subs, sub)
		if len(state.subs) == 0 {
			s.scheduleIdleClearLocked(sub.board, state)
		}
	}

	close(sub.events)
}

// scheduleIdleClearLocked (re)arms the idle clear of a board. The text and the
// attachments survive clear_delay after the last write or the last subscriber
// leaving, whichever happens last; a board with an open stream or nothing to
// clear is never scheduled.
func (s *Store) scheduleIdleClearLocked(name string, state *boardState) {
	if state.clearTimer != nil {
		state.clearTimer.Stop()
		state.clearTimer = nil
	}

	// Every schedule gets its own generation, captured by the timer before it
	// is started: a tick that fires just as a write re-arms the clear belongs
	// to a stale generation and must not empty the freshly written text.
	state.clearSeq++
	seq := state.clearSeq

	if len(state.subs) > 0 || (state.board.Text == "" && len(state.board.Files) == 0) {
		return
	}

	state.clearTimer = time.AfterFunc(s.cfg.ClearDelay, func() {
		s.clearIfIdle(name, seq)
	})
}

func (s *Store) clearIfIdle(name string, seq uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.boards[name]
	if !ok || state.clearSeq != seq {
		// The board was written or rescheduled after this tick was armed: the
		// newer schedule owns the board now, so this stale tick must not clear it.
		return
	}
	state.clearTimer = nil

	if len(state.subs) > 0 || (state.board.Text == "" && len(state.board.Files) == 0) {
		return
	}

	// Text and attachments share one life: an idle board is cleared whole.
	files := len(state.board.Files)
	s.removeBoardFilesLocked(state)
	state.board.Text = ""
	state.board.Files = nil
	state.board.Revision++
	state.board.UpdatedAt = time.Now().UTC()
	s.dirty = true

	logger.WithComponent("ghostboard").Debugf("cleared idle board %q after %s (%d file(s) removed)", name, s.cfg.ClearDelay, files)
}

func (s *Store) persistedBoardsLocked() []Board {
	boards := make([]Board, 0, len(s.boards))
	for _, state := range s.boards {
		// A board with attachments but no text is still stored: the files
		// follow the board, not its text.
		if state.board.Text == "" && len(state.board.Files) == 0 {
			continue
		}
		boards = append(boards, state.board)
	}
	sort.Slice(boards, func(i, j int) bool { return boards[i].Name < boards[j].Name })
	return boards
}

func (s *Store) markDirty() {
	s.mu.Lock()
	s.dirty = true
	s.mu.Unlock()
}
