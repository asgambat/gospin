package ghostboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asgambat/gospin/internal/sealedcrypto"
)

// sealedConfig returns a store configuration with room for an envelope and short
// timings, so a test never waits on a real clear delay.
func sealedConfig(dir string) Config {
	return Config{
		FilePath:        filepath.Join(dir, "ghostboard.json"),
		ClearDelay:      20 * time.Millisecond,
		SealedTombstone: time.Hour,
		MaxTextBytes:    4096,
		PersistInterval: 10 * time.Millisecond,
		SealedEnabled:   true,
	}
}

func newSealedStore(t *testing.T, mutate func(*Config)) *Store {
	t.Helper()

	cfg := sealedConfig(t.TempDir())
	if mutate != nil {
		mutate(&cfg)
	}
	store, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return store
}

// openSealedStore reopens the persistence file of a store, which is what a
// restart does.
func openSealedStore(t *testing.T, store *Store) *Store {
	t.Helper()

	if err := store.Flush(); err != nil {
		t.Fatalf("flushing before the reload: %v", err)
	}
	reloaded, err := New(store.cfg)
	if err != nil {
		t.Fatalf("reloading the store: %v", err)
	}
	return reloaded
}

// sealText seals a text for a board with a fresh key, returning the envelope and
// the key so a test can decrypt it again.
func sealText(t *testing.T, board, text string) (string, sealedcrypto.Key) {
	t.Helper()

	key, err := sealedcrypto.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	envelope, err := sealedcrypto.Seal(key, board, []byte(text))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	return envelope, key
}

func TestUpdateSealedCreatesASealedBoard(t *testing.T) {
	store := newSealedStore(t, nil)
	envelope, _ := sealText(t, "nota", "segreto")

	board, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope, ClientID: "cli"})
	if err != nil {
		t.Fatalf("UpdateSealed returned an error: %v", err)
	}
	if board.Mode != ModeSealed {
		t.Fatalf("board mode = %q, want %q", board.Mode, ModeSealed)
	}
	if board.Text != envelope {
		t.Fatal("the board does not carry the envelope it was written with")
	}
	if board.Revision != 1 {
		t.Fatalf("revision = %d, want 1", board.Revision)
	}

	current, err := store.Get("nota")
	if err != nil {
		t.Fatalf("Get returned an error: %v", err)
	}
	if current.Mode != ModeSealed || current.Text != envelope {
		t.Fatal("the stored board is not the sealed board that was written")
	}
}

func TestUpdateSealedRefusesAPlaintextWrite(t *testing.T) {
	store := newSealedStore(t, nil)
	envelope, _ := sealText(t, "nota", "segreto")
	if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope}); err != nil {
		t.Fatalf("UpdateSealed: %v", err)
	}

	if _, err := store.Update("nota", "plain text", ""); !errors.Is(err, ErrPlaintextWriteToSealed) {
		t.Fatalf("Update returned %v, want ErrPlaintextWriteToSealed", err)
	}

	current, err := store.Get("nota")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if current.Text != envelope {
		t.Fatal("the refused write changed the envelope")
	}
}

func TestUpdateSealedRefusesAPlainBoardWithText(t *testing.T) {
	store := newSealedStore(t, nil)
	if _, err := store.Update("nota", "already plain", ""); err != nil {
		t.Fatalf("Update: %v", err)
	}

	envelope, _ := sealText(t, "nota", "segreto")
	if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope}); !errors.Is(err, ErrSealedOverPlainText) {
		t.Fatalf("UpdateSealed returned %v, want ErrSealedOverPlainText", err)
	}

	current, err := store.Get("nota")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if current.Mode == ModeSealed || current.Text != "already plain" {
		t.Fatal("the refused sealed write changed the plain board")
	}
}

func TestUpdateSealedConvertsAnEmptyPlainBoard(t *testing.T) {
	store := newSealedStore(t, nil)
	if _, err := store.Update("nota", "", ""); err != nil {
		t.Fatalf("Update: %v", err)
	}

	envelope, _ := sealText(t, "nota", "segreto")
	if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope}); err != nil {
		t.Fatalf("UpdateSealed over an empty plain board returned an error: %v", err)
	}

	current, err := store.Get("nota")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if current.Mode != ModeSealed {
		t.Fatalf("board mode = %q, want %q", current.Mode, ModeSealed)
	}
}

func TestUpdateSealedRejectsAMalformedEnvelope(t *testing.T) {
	store := newSealedStore(t, nil)

	for _, envelope := range []string{"", "not an envelope", `{"v":9,"iv":"x","ct":"y"}`} {
		if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope}); err == nil {
			t.Fatalf("UpdateSealed accepted the envelope %q", envelope)
		}
	}

	current, err := store.Get("nota")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if current.Revision != 0 || current.Text != "" {
		t.Fatal("a rejected envelope still changed the board")
	}
}

func TestUpdateSealedRejectsAnOversizedEnvelope(t *testing.T) {
	store := newSealedStore(t, func(cfg *Config) { cfg.MaxTextBytes = 100 })
	envelope, _ := sealText(t, "nota", strings.Repeat("x", 500))

	if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope}); !errors.Is(err, ErrTextTooLarge) {
		t.Fatalf("UpdateSealed returned %v, want ErrTextTooLarge", err)
	}
}

func TestUpdateSealedIsANoOpForTheSameEnvelope(t *testing.T) {
	store := newSealedStore(t, nil)
	envelope, _ := sealText(t, "nota", "segreto")

	first, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope})
	if err != nil {
		t.Fatalf("UpdateSealed: %v", err)
	}
	second, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope})
	if err != nil {
		t.Fatalf("UpdateSealed: %v", err)
	}
	if second.Revision != first.Revision {
		t.Fatalf("the revision moved from %d to %d for an identical envelope", first.Revision, second.Revision)
	}
}

func TestUpdateSealedKeepsTheBurnFlagOfTheCreation(t *testing.T) {
	store := newSealedStore(t, nil)
	envelope, _ := sealText(t, "nota", "prima")
	if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope, Burn: true}); err != nil {
		t.Fatalf("UpdateSealed: %v", err)
	}

	updated, _ := sealText(t, "nota", "seconda")
	board, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: updated, Burn: false})
	if err != nil {
		t.Fatalf("UpdateSealed: %v", err)
	}
	if !board.Burn {
		t.Fatal("a later write cleared the burn flag of the board")
	}
}

func TestUpdateSealedBroadcastsTheEnvelope(t *testing.T) {
	store := newSealedStore(t, nil)
	sub, _, err := store.Subscribe("nota")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Close()

	envelope, _ := sealText(t, "nota", "segreto")
	if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope, ClientID: "cli"}); err != nil {
		t.Fatalf("UpdateSealed: %v", err)
	}

	select {
	case event := <-sub.Events():
		if event.Text != envelope || event.ClientID != "cli" {
			t.Fatalf("event = %+v, want the envelope written by cli", event)
		}
	case <-time.After(time.Second):
		t.Fatal("no event arrived for the sealed write")
	}
}

func TestOpenReturnsTheEnvelopeExactlyOnce(t *testing.T) {
	store := newSealedStore(t, nil)
	envelope, key := sealText(t, "nota", "brucia dopo la lettura")
	if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope, Burn: true}); err != nil {
		t.Fatalf("UpdateSealed: %v", err)
	}

	opened, err := store.Open("nota")
	if err != nil {
		t.Fatalf("Open returned an error: %v", err)
	}
	if opened.Text != envelope || opened.Mode != ModeSealed || !opened.Burn {
		t.Fatalf("Open returned %+v, want the envelope of a burn board", opened)
	}

	plaintext, err := sealedcrypto.Open(key, "nota", opened.Text)
	if err != nil {
		t.Fatalf("the handed out envelope does not decrypt: %v", err)
	}
	if string(plaintext) != "brucia dopo la lettura" {
		t.Fatalf("the envelope decrypts to %q", plaintext)
	}

	if _, err := store.Open("nota"); !errors.Is(err, ErrBoardBurned) {
		t.Fatalf("the second Open returned %v, want ErrBoardBurned", err)
	}
	if _, err := store.Get("nota"); !errors.Is(err, ErrBoardBurned) {
		t.Fatalf("Get after the burn returned %v, want ErrBoardBurned", err)
	}
}

func TestOpenSucceedsForExactlyOneConcurrentCaller(t *testing.T) {
	store := newSealedStore(t, nil)
	envelope, _ := sealText(t, "nota", "una sola volta")
	if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope, Burn: true}); err != nil {
		t.Fatalf("UpdateSealed: %v", err)
	}

	const callers = 16
	results := make(chan error, callers)
	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := store.Open("nota")
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	successes := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrBoardBurned):
		default:
			t.Fatalf("a concurrent Open returned %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("%d concurrent Open calls succeeded, want exactly 1", successes)
	}
}

func TestOpenRefusesEveryBoardThatIsNotABurnBoard(t *testing.T) {
	store := newSealedStore(t, nil)
	if _, err := store.Update("plain", "text", ""); err != nil {
		t.Fatalf("Update: %v", err)
	}
	envelope, _ := sealText(t, "sealed", "segreto")
	if _, err := store.UpdateSealed(SealedWrite{Name: "sealed", Envelope: envelope}); err != nil {
		t.Fatalf("UpdateSealed: %v", err)
	}

	for name, want := range map[string]error{
		"plain":  ErrBoardNotSealed,
		"sealed": ErrBoardNotSealed,
		"ghost":  ErrBoardNotFound,
	} {
		if _, err := store.Open(name); !errors.Is(err, want) {
			t.Fatalf("Open(%q) returned %v, want %v", name, err, want)
		}
	}
}

func TestOpenOnAnEmptyBurnBoardFindsNothing(t *testing.T) {
	store := newSealedStore(t, nil)

	// A burn board is normally created together with its envelope; an empty one
	// is reachable only through the idle burn, and it must not reserve its name.
	store.mu.Lock()
	store.boards["nota"] = &boardState{
		board: Board{Name: "nota", Mode: ModeSealed, Burn: true},
		subs:  make(map[*Subscription]struct{}),
	}
	store.mu.Unlock()

	if _, err := store.Open("nota"); !errors.Is(err, ErrBoardNotFound) {
		t.Fatalf("Open returned %v, want ErrBoardNotFound", err)
	}
}

func TestPlainReadOfABurnBoardIsRefused(t *testing.T) {
	store := newSealedStore(t, nil)
	envelope, _ := sealText(t, "nota", "segreto")
	if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope, Burn: true}); err != nil {
		t.Fatalf("UpdateSealed: %v", err)
	}

	if _, err := store.Get("nota"); !errors.Is(err, ErrBurnRequiresOpen) {
		t.Fatalf("Get returned %v, want ErrBurnRequiresOpen", err)
	}
	if _, _, err := store.Subscribe("nota"); !errors.Is(err, ErrBurnRequiresOpen) {
		t.Fatalf("Subscribe returned %v, want ErrBurnRequiresOpen", err)
	}
}

func TestBurnSurvivesAReload(t *testing.T) {
	store := newSealedStore(t, nil)
	envelope, _ := sealText(t, "nota", "effimera")
	if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope, Burn: true}); err != nil {
		t.Fatalf("UpdateSealed: %v", err)
	}
	if _, err := store.Open("nota"); err != nil {
		t.Fatalf("Open: %v", err)
	}

	reloaded := openSealedStore(t, store)

	if _, err := reloaded.Get("nota"); !errors.Is(err, ErrBoardBurned) {
		t.Fatalf("the reloaded store answered %v, want ErrBoardBurned", err)
	}
	if _, err := reloaded.Open("nota"); !errors.Is(err, ErrBoardBurned) {
		t.Fatalf("the reloaded store answered %v on Open, want ErrBoardBurned", err)
	}

	data, err := os.ReadFile(reloaded.cfg.FilePath)
	if err != nil {
		t.Fatalf("reading the persistence file: %v", err)
	}
	if strings.Contains(string(data), envelope) {
		t.Fatal("the burned envelope is still in the persistence file")
	}
	if !strings.Contains(string(data), "tombstones") {
		t.Fatalf("the persistence file carries no tombstone: %s", data)
	}
}

func TestOpenWithAFailedFlushRevealsNothingAndStaysRetryable(t *testing.T) {
	store := newSealedStore(t, nil)
	envelope, _ := sealText(t, "nota", "non deve uscire")
	if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope, Burn: true}); err != nil {
		t.Fatalf("UpdateSealed: %v", err)
	}

	// A file inside a path that cannot be written makes Flush fail without
	// depending on the permissions of the test user.
	blocker := filepath.Join(filepath.Dir(store.cfg.FilePath), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("creating the blocking file: %v", err)
	}
	goodPath := store.cfg.FilePath
	store.cfg.FilePath = filepath.Join(blocker, "ghostboard.json")

	board, err := store.Open("nota")
	if err == nil {
		t.Fatalf("Open returned the board %+v although the burn could not be persisted", board)
	}
	if board.Text != "" {
		t.Fatal("Open handed out the envelope although the burn failed")
	}

	store.cfg.FilePath = goodPath
	opened, err := store.Open("nota")
	if err != nil {
		t.Fatalf("Open after the failed flush returned an error: %v", err)
	}
	if opened.Text != envelope {
		t.Fatal("the retry did not return the envelope of the board")
	}
}

func TestTombstoneExpiryFreesTheName(t *testing.T) {
	store := newSealedStore(t, func(cfg *Config) { cfg.SealedTombstone = 20 * time.Millisecond })
	envelope, _ := sealText(t, "nota", "effimera")
	if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope, Burn: true}); err != nil {
		t.Fatalf("UpdateSealed: %v", err)
	}
	if _, err := store.Open("nota"); err != nil {
		t.Fatalf("Open: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err := store.Open("nota")
		if errors.Is(err, ErrBoardNotFound) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the tombstone never expired: Open returned %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	board, err := store.Get("nota")
	if err != nil {
		t.Fatalf("Get after the tombstone expired returned %v, want an empty board", err)
	}
	if board.Text != "" || board.Mode == ModeSealed {
		t.Fatalf("the freed name returned %+v, want an empty plain board", board)
	}

	fresh, _ := sealText(t, "nota", "nuova nota")
	if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: fresh}); err != nil {
		t.Fatalf("the freed name is not reusable: %v", err)
	}
}

func TestUnopenedBurnBoardSurvivesTheClearDelay(t *testing.T) {
	store := newSealedStore(t, func(cfg *Config) {
		cfg.ClearDelay = 20 * time.Millisecond
		cfg.SealedTombstone = 2 * time.Second
	})
	envelope, _ := sealText(t, "nota", "aspetta il lettore")
	if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope, Burn: true}); err != nil {
		t.Fatalf("UpdateSealed: %v", err)
	}

	time.Sleep(80 * time.Millisecond)

	if _, err := store.Get("nota"); !errors.Is(err, ErrBurnRequiresOpen) {
		t.Fatalf("Get after the clear delay returned %v, want the board still waiting for its reader", err)
	}
	if _, err := store.Open("nota"); err != nil {
		t.Fatalf("Open returned an error after the clear delay: %v", err)
	}
}

func TestIdleBurnBoardIsDestroyedAfterItsOwnWindow(t *testing.T) {
	store := newSealedStore(t, func(cfg *Config) {
		cfg.ClearDelay = 20 * time.Millisecond
		cfg.SealedTombstone = 30 * time.Millisecond
	})
	envelope, _ := sealText(t, "nota", "non letta")
	if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope, Burn: true}); err != nil {
		t.Fatalf("UpdateSealed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err := store.Get("nota")
		if errors.Is(err, ErrBoardBurned) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the unread burn board was never destroyed: Get returned %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	data, err := os.ReadFile(filepath.Join(filepath.Dir(store.cfg.FilePath), "ghostboard.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reading the persistence file: %v", err)
	}
	if strings.Contains(string(data), envelope) {
		t.Fatal("the envelope of the idle burn board is still in the persistence file")
	}
}

func TestIdleClearKeepsASealedBoardSealed(t *testing.T) {
	store := newSealedStore(t, func(cfg *Config) { cfg.ClearDelay = 20 * time.Millisecond })
	envelope, _ := sealText(t, "nota", "sigillata")
	if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope}); err != nil {
		t.Fatalf("UpdateSealed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		board, err := store.Get("nota")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if board.Text == "" {
			if board.Mode != ModeSealed {
				t.Fatalf("the cleared board became %q, want %q", board.Mode, ModeSealed)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the idle clear never emptied the sealed board")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := store.Update("nota", "plain", ""); !errors.Is(err, ErrPlaintextWriteToSealed) {
		t.Fatalf("Update on the cleared sealed board returned %v, want ErrPlaintextWriteToSealed", err)
	}
}

func TestSealedBoardRefusesAttachments(t *testing.T) {
	store := newSealedStore(t, func(cfg *Config) {
		cfg.Files = FileConfig{
			Enabled:         true,
			Root:            filepath.Join(filepath.Dir(cfg.FilePath), "files"),
			MaxFileBytes:    1024,
			MaxBoardBytes:   4096,
			MaxTotalBytes:   8192,
			MaxFileCount:    3,
			TTL:             time.Hour,
			TransferTimeout: time.Minute,
		}
	})
	envelope, _ := sealText(t, "nota", "sigillata")
	if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope}); err != nil {
		t.Fatalf("UpdateSealed: %v", err)
	}

	if _, err := store.WriteFile(context.Background(), "nota", "x.txt", "", bytes.NewReader([]byte("x"))); !errors.Is(err, ErrSealedBoardFiles) {
		t.Fatalf("WriteFile returned %v, want ErrSealedBoardFiles", err)
	}
	if _, err := store.Files("nota"); !errors.Is(err, ErrSealedBoardFiles) {
		t.Fatalf("Files returned %v, want ErrSealedBoardFiles", err)
	}
	if err := store.DeleteFile("nota", strings.Repeat("a", 26), ""); !errors.Is(err, ErrSealedBoardFiles) {
		t.Fatalf("DeleteFile returned %v, want ErrSealedBoardFiles", err)
	}
	if _, err := store.DeleteFiles("nota", ""); !errors.Is(err, ErrSealedBoardFiles) {
		t.Fatalf("DeleteFiles returned %v, want ErrSealedBoardFiles", err)
	}
	if _, err := store.ArchiveFiles(context.Background(), "nota", &bytes.Buffer{}); !errors.Is(err, ErrSealedBoardFiles) {
		t.Fatalf("ArchiveFiles returned %v, want ErrSealedBoardFiles", err)
	}
}

func TestSealedDisabledRefusesWritesAndKeepsReads(t *testing.T) {
	store := newSealedStore(t, nil)
	envelope, _ := sealText(t, "nota", "esistente")
	if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope}); err != nil {
		t.Fatalf("UpdateSealed: %v", err)
	}
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	disabled, err := New(store.cfg)
	if err != nil {
		t.Fatalf("reloading the store: %v", err)
	}
	disabled.cfg.SealedEnabled = false

	if _, err := disabled.UpdateSealed(SealedWrite{Name: "altra", Envelope: envelope}); !errors.Is(err, ErrSealedDisabled) {
		t.Fatalf("UpdateSealed returned %v, want ErrSealedDisabled", err)
	}

	board, err := disabled.Get("nota")
	if err != nil {
		t.Fatalf("Get returned %v, want the stored envelope", err)
	}
	if board.Text != envelope {
		t.Fatal("a disabled sealed mode stopped serving the stored envelope")
	}
}

func TestNewDefaultsTheSealedTombstoneAndRejectsANegativeOne(t *testing.T) {
	store := newSealedStore(t, func(cfg *Config) { cfg.SealedTombstone = 0 })
	if store.cfg.SealedTombstone != DefaultSealedTombstone {
		t.Fatalf("tombstone = %s, want %s", store.cfg.SealedTombstone, DefaultSealedTombstone)
	}

	cfg := sealedConfig(t.TempDir())
	cfg.SealedTombstone = -time.Second
	if _, err := New(cfg); err == nil {
		t.Fatal("New accepted a negative sealed burn tombstone")
	}
}

func TestLoadDropsAnExpiredTombstoneAndAnExpiredBoardEntry(t *testing.T) {
	cfg := sealedConfig(t.TempDir())
	cfg.SealedTombstone = time.Hour

	envelope, _ := sealText(t, "nota", "vecchia")
	now := time.Now().UTC()
	document, err := json.Marshal(fileDocument{
		Boards: []Board{{Name: "nota", Text: envelope, Mode: ModeSealed, Burn: true, Revision: 2}},
		Tombstones: []tombstone{
			{Name: "scaduta", BurnedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
			{Name: "fresca", BurnedAt: now},
			{Name: "nota", BurnedAt: now},
		},
	})
	if err != nil {
		t.Fatalf("encoding the persistence file: %v", err)
	}

	if err := os.WriteFile(cfg.FilePath, document, 0o600); err != nil {
		t.Fatalf("writing the persistence file: %v", err)
	}

	store, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := store.Get("scaduta"); err != nil {
		t.Fatalf("an expired tombstone still answers %v", err)
	}
	if _, err := store.Get("fresca"); !errors.Is(err, ErrBoardBurned) {
		t.Fatalf("a live tombstone answered %v, want ErrBoardBurned", err)
	}
	if _, err := store.Get("nota"); !errors.Is(err, ErrBoardBurned) {
		t.Fatalf("a name that is both a board and a tombstone answered %v, want ErrBoardBurned", err)
	}
}

func TestLoadRejectsAnInvalidTombstoneName(t *testing.T) {
	cfg := sealedConfig(t.TempDir())
	document := `{"boards":[],"tombstones":[{"name":"not a board","burned_at":"` +
		time.Now().UTC().Format(time.RFC3339) + `"}]}`

	if err := os.WriteFile(cfg.FilePath, []byte(document), 0o600); err != nil {
		t.Fatalf("writing the persistence file: %v", err)
	}

	if _, err := New(cfg); err == nil {
		t.Fatal("New accepted a document with an invalid tombstone name")
	}
}

func TestPlainBoardPersistsWithoutTheSealedFields(t *testing.T) {
	store := newSealedStore(t, nil)
	if _, err := store.Update("nota", "testo", ""); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	data, err := os.ReadFile(store.cfg.FilePath)
	if err != nil {
		t.Fatalf("reading the persistence file: %v", err)
	}
	if strings.Contains(string(data), "mode") || strings.Contains(string(data), "burn") {
		t.Fatalf("a plain board carries sealed fields: %s", data)
	}
}

func TestRestoreBurnAfterFailedFlushIsConditionalOnTheRevision(t *testing.T) {
	store := newSealedStore(t, nil)
	envelope, _ := sealText(t, "nota", "nascosto")
	if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope, Burn: true}); err != nil {
		t.Fatalf("UpdateSealed: %v", err)
	}

	store.mu.Lock()
	state := store.boards["nota"]
	state.clearTimer.Stop()
	burnedRevision := state.board.Revision
	store.mu.Unlock()

	// What Open leaves behind before restoring: the board emptied and its
	// tombstone recorded, with no write in flight.
	store.mu.Lock()
	state.board.Text = ""
	store.tombstones["nota"] = time.Now().UTC()
	store.restoreBurnAfterFailedFlushLocked("nota", state, envelope, burnedRevision)
	if state.board.Text != envelope {
		t.Fatal("a pristine board was not restored to its envelope")
	}
	if _, live := store.tombstones["nota"]; live {
		t.Fatal("the tombstone survived the restore: the board would answer ErrBoardBurned forever")
	}
	store.mu.Unlock()

	// A write landed while the flush of the failed burn ran: the revision moved
	// and the text is somebody else's, so the restore must not touch it.
	concurrent, _ := sealText(t, "nota", "scritto durante il flush")
	store.mu.Lock()
	state.board.Text = concurrent
	state.board.Revision = burnedRevision + 1
	store.tombstones["nota"] = time.Now().UTC()
	store.mu.Unlock()

	store.mu.Lock()
	store.restoreBurnAfterFailedFlushLocked("nota", state, envelope, burnedRevision)
	store.mu.Unlock()

	if state.board.Text != concurrent {
		t.Fatal("the restore overwrote a write that happened after the burn started")
	}
	if _, live := store.tombstones["nota"]; !live {
		t.Fatal("the tombstone of an unrestored burn is gone: the name would free up too early")
	}
}

func TestIdleBurnKeepsTheTombstoneOffThePersistLoop(t *testing.T) {
	store := newSealedStore(t, nil)
	envelope, _ := sealText(t, "nota", "effimera")
	if _, err := store.UpdateSealed(SealedWrite{Name: "nota", Envelope: envelope, Burn: true}); err != nil {
		t.Fatalf("UpdateSealed: %v", err)
	}

	store.mu.Lock()
	state := store.boards["nota"]
	seq := state.clearSeq
	state.clearTimer.Stop()
	store.mu.Unlock()
	if seq == 0 {
		t.Fatal("UpdateSealed did not arm the idle clear")
	}

	// The idle tick burns by hand: the tombstone must reach the persistence
	// file before the tick returns, without waiting for the persist interval.
	store.clearIfIdle("nota", seq)

	data, err := os.ReadFile(store.cfg.FilePath)
	if err != nil {
		t.Fatalf("reading the persistence file: %v", err)
	}
	if !strings.Contains(string(data), "tombstones") {
		t.Fatalf("the tombstone of an idle burn is not on disk yet: %s", data)
	}

	reloaded, err := New(store.cfg)
	if err != nil {
		t.Fatalf("reloading the store: %v", err)
	}
	if _, err := reloaded.Open("nota"); !errors.Is(err, ErrBoardBurned) {
		t.Fatalf("a restarted store answered an idle-burned board with %v, want ErrBoardBurned", err)
	}
}
