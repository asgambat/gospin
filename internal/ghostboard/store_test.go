package ghostboard

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// testConfig returns a store configuration with short timings for tests.
func testConfig(dir string) Config {
	return Config{
		FilePath:        filepath.Join(dir, "ghostboard.json"),
		ClearDelay:      30 * time.Millisecond,
		MaxTextBytes:    64,
		PersistInterval: 10 * time.Millisecond,
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := New(testConfig(t.TempDir()))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return store
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	base := testConfig(t.TempDir())

	tests := []struct {
		mutate func(*Config)
		name   string
	}{
		{name: "zero clear delay", mutate: func(c *Config) { c.ClearDelay = 0 }},
		{name: "negative clear delay", mutate: func(c *Config) { c.ClearDelay = -time.Second }},
		{name: "zero max text", mutate: func(c *Config) { c.MaxTextBytes = 0 }},
		{name: "zero persist interval", mutate: func(c *Config) { c.PersistInterval = 0 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			tt.mutate(&cfg)
			if _, err := New(cfg); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}

func TestGetUnknownBoard(t *testing.T) {
	store := newTestStore(t)

	board, err := store.Get("lavoro")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if board.Name != "lavoro" {
		t.Errorf("Name = %q, want lavoro", board.Name)
	}
	if board.Text != "" || board.Revision != 0 {
		t.Errorf("unexpected content for unknown board: %+v", board)
	}
}

func TestUpdateAndGet(t *testing.T) {
	store := newTestStore(t)

	board, err := store.Update("lavoro", "ciao", "client-1")
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if board.Revision != 1 {
		t.Errorf("Revision = %d, want 1", board.Revision)
	}
	if board.Text != "ciao" {
		t.Errorf("Text = %q, want ciao", board.Text)
	}
	if board.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set")
	}

	got, err := store.Get("lavoro")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !reflect.DeepEqual(got, board) {
		t.Errorf("Get() = %+v, want %+v", got, board)
	}
}

func TestUpdateSameTextKeepsRevision(t *testing.T) {
	store := newTestStore(t)

	first, err := store.Update("note", "same", "")
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	second, err := store.Update("note", "same", "")
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if second.Revision != first.Revision {
		t.Errorf("Revision = %d, want unchanged %d", second.Revision, first.Revision)
	}
}

func TestUpdateEmptyTextClearsBoard(t *testing.T) {
	store := newTestStore(t)

	if _, err := store.Update("note", "contenuto", ""); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	board, err := store.Update("note", "", "")
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if board.Text != "" {
		t.Errorf("Text = %q, want empty", board.Text)
	}
	if board.Revision != 2 {
		t.Errorf("Revision = %d, want 2", board.Revision)
	}
}

func TestUpdateRejectsInvalidBoardName(t *testing.T) {
	store := newTestStore(t)

	names := []string{"", "-lead", ".hidden", "a/b", strings.Repeat("a", 65), "spazio nome"}
	for _, name := range names {
		if _, err := store.Update(name, "text", ""); !errors.Is(err, ErrInvalidBoardName) {
			t.Errorf("Update(%q) error = %v, want ErrInvalidBoardName", name, err)
		}
	}
}

func TestUpdateRejectsOversizedText(t *testing.T) {
	store := newTestStore(t)

	_, err := store.Update("note", strings.Repeat("x", 65), "")
	if !errors.Is(err, ErrTextTooLarge) {
		t.Fatalf("Update() error = %v, want ErrTextTooLarge", err)
	}
}

func TestSubscribeReceivesUpdates(t *testing.T) {
	store := newTestStore(t)

	sub, snapshot, err := store.Subscribe("note")
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer sub.Close()

	if snapshot.Text != "" || snapshot.Revision != 0 {
		t.Errorf("unexpected snapshot: %+v", snapshot)
	}

	updated, err := store.Update("note", "live", "tab-1")
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	select {
	case event := <-sub.Events():
		if event.Text != "live" || event.Revision != updated.Revision {
			t.Errorf("event board = %+v, want %+v", event.Board, updated)
		}
		if event.ClientID != "tab-1" {
			t.Errorf("ClientID = %q, want tab-1", event.ClientID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the event")
	}
}

func TestBoardsAreIndependent(t *testing.T) {
	store := newTestStore(t)

	if _, err := store.Update("alpha", "one", ""); err != nil {
		t.Fatalf("Update(alpha) error = %v", err)
	}
	if _, err := store.Update("beta", "two", ""); err != nil {
		t.Fatalf("Update(beta) error = %v", err)
	}

	alpha, err := store.Get("alpha")
	if err != nil {
		t.Fatalf("Get(alpha) error = %v", err)
	}
	beta, err := store.Get("beta")
	if err != nil {
		t.Fatalf("Get(beta) error = %v", err)
	}
	if alpha.Text != "one" || beta.Text != "two" {
		t.Errorf("boards leaked into each other: alpha=%q beta=%q", alpha.Text, beta.Text)
	}
}

func TestUnsubscribeClearsIdleBoard(t *testing.T) {
	store := newTestStore(t)

	sub, _, err := store.Subscribe("note")
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	if _, err := store.Update("note", "temporaneo", ""); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	sub.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		board, err := store.Get("note")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if board.Text == "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("board was not cleared after the clear delay")
}

func TestSubscribeCancelsPendingClear(t *testing.T) {
	store := newTestStore(t)

	first, _, err := store.Subscribe("note")
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	if _, err := store.Update("note", "persistente", ""); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	first.Close()

	// Reconnect well before the clear delay elapses.
	time.Sleep(10 * time.Millisecond)
	second, snapshot, err := store.Subscribe("note")
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer second.Close()
	if snapshot.Text != "persistente" {
		t.Errorf("snapshot.Text = %q, want persistente", snapshot.Text)
	}

	time.Sleep(60 * time.Millisecond)
	board, err := store.Get("note")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if board.Text != "persistente" {
		t.Errorf("board was cleared despite a subscriber: %+v", board)
	}
}

func TestUpdateSchedulesIdleClearWithoutSubscribers(t *testing.T) {
	store := newTestStore(t)

	if _, err := store.Update("note", "scritto dalla CLI", ""); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		board, err := store.Get("note")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if board.Text == "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("board written without subscribers was not cleared after the clear delay")
}

func TestUpdateRearmsPendingIdleClear(t *testing.T) {
	cfg := testConfig(t.TempDir())
	// Wide margins on purpose: a slow CI must not turn the timing check into a
	// failure, so each step keeps a few hundred milliseconds of slack.
	cfg.ClearDelay = 500 * time.Millisecond
	store, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := store.Update("note", "prima", ""); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	// Write again late enough that the original deadline has passed by the
	// time the next check runs, but the re-armed one has not.
	time.Sleep(300 * time.Millisecond)
	if _, err := store.Update("note", "seconda", ""); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	board, err := store.Get("note")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if board.Text != "seconda" {
		t.Fatalf("board cleared on the original deadline instead of the re-armed one: %+v", board)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		board, err := store.Get("note")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if board.Text == "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("board was not cleared after the re-armed clear delay")
}

func TestStaleClearTickDoesNotClearBoard(t *testing.T) {
	store := newTestStore(t)

	if _, err := store.Update("note", "nuovo", ""); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	store.mu.Lock()
	state := store.boards["note"]
	seq := state.clearSeq
	timer := state.clearTimer
	store.mu.Unlock()
	if seq == 0 || timer == nil {
		t.Fatal("Update() did not arm the idle clear")
	}
	// The test fires the ticks by hand, so the armed timer is not needed.
	timer.Stop()

	// A tick that fired just as a write re-armed the clear belongs to the
	// previous generation and must not empty the board.
	store.clearIfIdle("note", seq-1)

	board, err := store.Get("note")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if board.Text != "nuovo" {
		t.Errorf("Text = %q, want the text untouched by the stale tick", board.Text)
	}

	// The current generation must still clear, so the guard rejects stale
	// ticks only instead of disabling the idle clear.
	store.clearIfIdle("note", seq)

	board, err = store.Get("note")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if board.Text != "" {
		t.Errorf("Text = %q, want the current tick to clear the board", board.Text)
	}
}

func TestSubscriptionCloseIsIdempotent(t *testing.T) {
	store := newTestStore(t)

	sub, _, err := store.Subscribe("note")
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	sub.Close()
	sub.Close()

	if _, ok := <-sub.Events(); ok {
		t.Error("expected the events channel to be closed")
	}
}

func TestFlushAndReload(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)

	store, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := store.Update("note", "persistito", ""); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	reloaded, err := New(cfg)
	if err != nil {
		t.Fatalf("New() after restart error = %v", err)
	}
	board, err := reloaded.Get("note")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if board.Text != "persistito" || board.Revision != 1 {
		t.Errorf("reloaded board = %+v, want text persistito revision 1", board)
	}
}

func TestFlushSkipsEmptyBoards(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)

	store, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := store.Update("note", "testo", ""); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if _, err := store.Update("note", "", ""); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	data, err := os.ReadFile(cfg.FilePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var doc fileDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(doc.Boards) != 0 {
		t.Errorf("persisted %d board(s), want none: %+v", len(doc.Boards), doc.Boards)
	}
}

func TestFlushWithoutChangesSkipsWrite(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)

	store, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if _, err := os.Stat(cfg.FilePath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected no file to be written, stat error = %v", err)
	}
}

func TestNewRejectsCorruptFile(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	if err := os.WriteFile(cfg.FilePath, []byte("not json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := New(cfg); err == nil {
		t.Fatal("expected an error for a corrupt persistence file")
	}
}

func TestNewRejectsInvalidBoardInFile(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	data := []byte(`{"boards":[{"name":"bad/name","text":"x"}]}`)
	if err := os.WriteFile(cfg.FilePath, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := New(cfg); err == nil {
		t.Fatal("expected an error for an invalid board name")
	}
}

func TestNewRejectsOversizedBoardInFile(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	data := []byte(`{"boards":[{"name":"note","text":"` + strings.Repeat("x", 65) + `"}]}`)
	if err := os.WriteFile(cfg.FilePath, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := New(cfg); err == nil {
		t.Fatal("expected an error for an oversized board")
	}
}

func TestStartPersistenceFlushesAndStops(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.PersistInterval = 10 * time.Millisecond

	store, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := store.Update("note", "automatico", ""); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	store.StartPersistence(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, readErr := os.ReadFile(cfg.FilePath)
		if readErr == nil && strings.Contains(string(data), "automatico") {
			cancel()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	t.Fatal("the persistence loop did not write the board")
}

func TestConcurrentUpdatesAreSerialized(t *testing.T) {
	store := newTestStore(t)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := store.Update("shared", strings.Repeat("x", i%10)+"-"+time.Now().Format("150405.000000"), ""); err != nil {
				t.Errorf("Update() error = %v", err)
			}
		}(i)
	}
	wg.Wait()

	board, err := store.Get("shared")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if board.Revision == 0 {
		t.Error("expected the revision to advance")
	}
}
