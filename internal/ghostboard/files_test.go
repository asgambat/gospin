package ghostboard

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// newFileTestStore builds a store with attachments in a temporary directory.
// tune can lower the limits or the timings of a test.
func newFileTestStore(t *testing.T, tune func(*Config)) *Store {
	t.Helper()

	dir := t.TempDir()
	cfg := Config{
		FilePath:        filepath.Join(dir, "ghostboard.json"),
		ClearDelay:      time.Hour,
		MaxTextBytes:    1024,
		PersistInterval: time.Second,
		Files: FileConfig{
			Enabled:         true,
			Root:            filepath.Join(dir, "files"),
			MaxFileBytes:    1 << 20,
			MaxBoardBytes:   4 << 20,
			MaxTotalBytes:   8 << 20,
			MaxFileCount:    3,
			TTL:             time.Hour,
			TransferTimeout: time.Minute,
		},
	}
	if tune != nil {
		tune(&cfg)
	}

	store, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return store
}

func TestWriteFileStoresAndLists(t *testing.T) {
	store := newFileTestStore(t, nil)

	file, err := store.WriteFile(context.Background(), "team", "../../etc/hosts", "", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if file.Name != "hosts" {
		t.Errorf("Name = %q, want the base name of the upload", file.Name)
	}
	if file.Size != 5 {
		t.Errorf("Size = %d, want 5", file.Size)
	}
	if file.ContentType != "text/plain" {
		t.Errorf("ContentType = %q, want text/plain", file.ContentType)
	}
	if !fileIDPattern.MatchString(file.ID) {
		t.Errorf("ID = %q, want 26 base32 characters", file.ID)
	}
	if got, want := file.ExpiresAt.Sub(file.CreatedAt), time.Hour; got != want {
		t.Errorf("expiry - creation = %v, want %v", got, want)
	}

	path := filepath.Join(store.cfg.Files.Root, "team", file.ID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the stored file: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("stored bytes = %q, want hello", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the stored file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", info.Mode().Perm())
	}

	second, err := store.WriteFile(context.Background(), "team", "b.txt", "", strings.NewReader("second"))
	if err != nil {
		t.Fatalf("second WriteFile() error = %v", err)
	}

	files, err := store.Files("team")
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("Files() = %d, want 2", len(files))
	}
	if files[0].ID != second.ID {
		t.Errorf("Files()[0] = %s, want the newest file %s", files[0].ID, second.ID)
	}

	// The board payload carries the attachments too.
	board, err := store.Get("team")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(board.Files) != 2 {
		t.Errorf("board.Files = %d, want 2", len(board.Files))
	}

	// A caller cannot mutate the store through the returned slice.
	board.Files[0].Name = "changed"
	again, err := store.Files("team")
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}
	if again[0].Name == "changed" {
		t.Error("Files() returned a slice the store still shares")
	}
}

func TestWriteFileRejectsLargeEmptyAndUnnamedUploads(t *testing.T) {
	store := newFileTestStore(t, func(cfg *Config) { cfg.Files.MaxFileBytes = 4 })

	t.Run("too large", func(t *testing.T) {
		_, err := store.WriteFile(context.Background(), "team", "big.txt", "", strings.NewReader("12345"))
		if !errors.Is(err, ErrFileTooLarge) {
			t.Fatalf("WriteFile() error = %v, want ErrFileTooLarge", err)
		}
		assertBoardDirEmpty(t, store, "team")
	})

	t.Run("exactly at the limit", func(t *testing.T) {
		file, err := store.WriteFile(context.Background(), "team", "ok.txt", "", strings.NewReader("1234"))
		if err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		if file.Size != 4 {
			t.Errorf("Size = %d, want 4", file.Size)
		}
	})

	t.Run("empty", func(t *testing.T) {
		_, err := store.WriteFile(context.Background(), "team", "empty.txt", "", strings.NewReader(""))
		if !errors.Is(err, ErrEmptyFile) {
			t.Fatalf("WriteFile() error = %v, want ErrEmptyFile", err)
		}
	})

	t.Run("unnamed", func(t *testing.T) {
		_, err := store.WriteFile(context.Background(), "team", "..", "", strings.NewReader("x"))
		if !errors.Is(err, ErrInvalidFileName) {
			t.Fatalf("WriteFile() error = %v, want ErrInvalidFileName", err)
		}
	})

	t.Run("invalid board", func(t *testing.T) {
		_, err := store.WriteFile(context.Background(), "not a board", "a.txt", "", strings.NewReader("x"))
		if !errors.Is(err, ErrInvalidBoardName) {
			t.Fatalf("WriteFile() error = %v, want ErrInvalidBoardName", err)
		}
	})
}

func TestWriteFileQuotas(t *testing.T) {
	t.Run("count", func(t *testing.T) {
		store := newFileTestStore(t, func(cfg *Config) {
			cfg.Files.MaxFileCount = 2
			cfg.Files.MaxBoardBytes = 1 << 20
			cfg.Files.MaxTotalBytes = 1 << 20
		})

		for i := 0; i < 2; i++ {
			if _, err := store.WriteFile(context.Background(), "team", "a.txt", "", strings.NewReader("x")); err != nil {
				t.Fatalf("WriteFile() #%d error = %v", i, err)
			}
		}

		_, err := store.WriteFile(context.Background(), "team", "c.txt", "", strings.NewReader("x"))
		if !errors.Is(err, ErrFileCountExceeded) {
			t.Fatalf("WriteFile() error = %v, want ErrFileCountExceeded", err)
		}
	})

	t.Run("board bytes", func(t *testing.T) {
		store := newFileTestStore(t, func(cfg *Config) {
			cfg.Files.MaxFileBytes = 8
			cfg.Files.MaxBoardBytes = 10
			cfg.Files.MaxTotalBytes = 100
		})

		if _, err := store.WriteFile(context.Background(), "team", "a.txt", "", strings.NewReader("123456")); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		_, err := store.WriteFile(context.Background(), "team", "b.txt", "", strings.NewReader("123456"))
		if !errors.Is(err, ErrBoardFileQuotaExceeded) {
			t.Fatalf("WriteFile() error = %v, want ErrBoardFileQuotaExceeded", err)
		}
	})

	t.Run("instance bytes", func(t *testing.T) {
		store := newFileTestStore(t, func(cfg *Config) {
			cfg.Files.MaxFileBytes = 8
			cfg.Files.MaxBoardBytes = 8
			cfg.Files.MaxTotalBytes = 8
		})

		if _, err := store.WriteFile(context.Background(), "one", "a.txt", "", strings.NewReader("123456")); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		_, err := store.WriteFile(context.Background(), "two", "b.txt", "", strings.NewReader("123456"))
		if !errors.Is(err, ErrTotalFileQuotaExceeded) {
			t.Fatalf("WriteFile() error = %v, want ErrTotalFileQuotaExceeded", err)
		}
	})

	t.Run("an expired file frees its quota", func(t *testing.T) {
		store := newFileTestStore(t, func(cfg *Config) {
			cfg.Files.MaxFileBytes = 8
			cfg.Files.MaxBoardBytes = 8
			cfg.Files.MaxTotalBytes = 8
			cfg.Files.MaxFileCount = 1
			cfg.Files.TTL = time.Millisecond
		})

		if _, err := store.WriteFile(context.Background(), "team", "old.txt", "", strings.NewReader("1234")); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		time.Sleep(10 * time.Millisecond)

		if _, err := store.WriteFile(context.Background(), "team", "new.txt", "", strings.NewReader("1234")); err != nil {
			t.Fatalf("WriteFile() error = %v, want the expired file to be swept first", err)
		}
	})
}

func TestDeleteFileAndDeleteFiles(t *testing.T) {
	store := newFileTestStore(t, nil)

	first, err := store.WriteFile(context.Background(), "team", "a.txt", "", strings.NewReader("a"))
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	second, err := store.WriteFile(context.Background(), "team", "b.txt", "", strings.NewReader("b"))
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := store.DeleteFile("team", first.ID, ""); err != nil {
		t.Fatalf("DeleteFile() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.cfg.Files.Root, "team", first.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the bytes of the deleted file are still on disk: %v", err)
	}
	if err := store.DeleteFile("team", first.ID, ""); !errors.Is(err, ErrFileNotFound) {
		t.Errorf("second DeleteFile() error = %v, want ErrFileNotFound", err)
	}
	if err := store.DeleteFile("team", "nope", ""); !errors.Is(err, ErrInvalidFileID) {
		t.Errorf("DeleteFile() with an invalid id = %v, want ErrInvalidFileID", err)
	}

	removed, err := store.DeleteFiles("team", "")
	if err != nil {
		t.Fatalf("DeleteFiles() error = %v", err)
	}
	if removed != 1 {
		t.Errorf("DeleteFiles() = %d, want 1", removed)
	}
	files, err := store.Files("team")
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}
	if len(files) != 0 {
		t.Errorf("Files() = %v, want none", files)
	}
	if _, err := os.Stat(filepath.Join(store.cfg.Files.Root, "team", second.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the bytes of the second file are still on disk: %v", err)
	}
}

func TestSweepFilesDropsExpiredAttachments(t *testing.T) {
	store := newFileTestStore(t, func(cfg *Config) { cfg.Files.TTL = 5 * time.Millisecond })

	file, err := store.WriteFile(context.Background(), "team", "short.txt", "", strings.NewReader("short"))
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	time.Sleep(15 * time.Millisecond)

	if err := store.SweepFiles(); err != nil {
		t.Fatalf("SweepFiles() error = %v", err)
	}

	files, err := store.Files("team")
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}
	if len(files) != 0 {
		t.Errorf("Files() = %v, want the expired file gone", files)
	}
	if _, err := os.Stat(filepath.Join(store.cfg.Files.Root, "team", file.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the expired file is still on disk: %v", err)
	}
}

func TestIdleClearDeletesTextAndFiles(t *testing.T) {
	store := newFileTestStore(t, func(cfg *Config) { cfg.ClearDelay = 20 * time.Millisecond })

	if _, err := store.Update("team", "text that will go", ""); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	file, err := store.WriteFile(context.Background(), "team", "a.txt", "", strings.NewReader("a"))
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		board, err := store.Get("team")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if board.Text == "" && len(board.Files) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the idle clear did not run: %+v", board)
		}
		time.Sleep(5 * time.Millisecond)
	}

	if _, err := os.Stat(filepath.Join(store.cfg.Files.Root, "team", file.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the file of the cleared board is still on disk: %v", err)
	}
}

func TestIdleClearDeletesFileOnlyBoard(t *testing.T) {
	store := newFileTestStore(t, func(cfg *Config) { cfg.ClearDelay = 20 * time.Millisecond })

	// A board born from an upload has no text at all, and must be cleared like
	// any other: the attachments follow the board, not its text.
	file, err := store.WriteFile(context.Background(), "team", "a.txt", "", strings.NewReader("a"))
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if board, err := store.Get("team"); err != nil || board.Text != "" || len(board.Files) != 1 {
		t.Fatalf("the board was not created by the upload alone: %+v, %v", board, err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		board, err := store.Get("team")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if len(board.Files) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the idle clear did not delete the files of a board without text: %+v", board)
		}
		time.Sleep(5 * time.Millisecond)
	}

	if _, err := os.Stat(filepath.Join(store.cfg.Files.Root, "team", file.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the file of the cleared board is still on disk: %v", err)
	}
}

func TestUploadCountsAsBoardActivity(t *testing.T) {
	store := newFileTestStore(t, func(cfg *Config) { cfg.ClearDelay = 40 * time.Millisecond })

	if _, err := store.Update("team", "text", ""); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	// The upload re-arms the clear, so the text written before it survives one
	// more full delay.
	if _, err := store.WriteFile(context.Background(), "team", "a.txt", "", strings.NewReader("a")); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	board, err := store.Get("team")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if board.Text != "text" {
		t.Errorf("Text = %q immediately after the upload, want the board still written", board.Text)
	}
}

func TestWriteFileDisabled(t *testing.T) {
	store := newFileTestStore(t, func(cfg *Config) { cfg.Files.Enabled = false })

	if _, err := store.WriteFile(context.Background(), "team", "a.txt", "", strings.NewReader("a")); !errors.Is(err, ErrFilesDisabled) {
		t.Fatalf("WriteFile() error = %v, want ErrFilesDisabled", err)
	}
	files, err := store.Files("team")
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}
	if len(files) != 0 {
		t.Errorf("Files() = %v, want none", files)
	}
	if err := store.ReconcileFiles(); err != nil {
		t.Fatalf("ReconcileFiles() error = %v", err)
	}
}

func TestOpenFileRefusesSymlinks(t *testing.T) {
	store := newFileTestStore(t, nil)

	file, err := store.WriteFile(context.Background(), "team", "a.txt", "", strings.NewReader("a"))
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	path := filepath.Join(store.cfg.Files.Root, "team", file.ID)
	target := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatalf("writing the target: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("removing the stored file: %v", err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}

	if _, _, err := store.OpenFile("team", file.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("OpenFile() error = %v, want ErrFileNotFound for a symlink", err)
	}
}

func TestFilesPersistAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		FilePath:        filepath.Join(dir, "ghostboard.json"),
		ClearDelay:      time.Hour,
		MaxTextBytes:    1024,
		PersistInterval: time.Second,
		Files: FileConfig{
			Enabled:         true,
			Root:            filepath.Join(dir, "files"),
			MaxFileBytes:    1 << 20,
			MaxBoardBytes:   1 << 20,
			MaxTotalBytes:   1 << 20,
			MaxFileCount:    5,
			TTL:             time.Hour,
			TransferTimeout: time.Minute,
		},
	}

	store, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	stored, err := store.WriteFile(context.Background(), "team", "persisted.txt", "", strings.NewReader("persisted"))
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	data, err := os.ReadFile(cfg.FilePath)
	if err != nil {
		t.Fatalf("reading the persistence file: %v", err)
	}
	if !strings.Contains(string(data), stored.ID) {
		t.Errorf("the persistence file does not carry the attachment: %s", data)
	}

	reopened, err := New(cfg)
	if err != nil {
		t.Fatalf("reopening the store: %v", err)
	}
	files, err := reopened.Files("team")
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}
	if len(files) != 1 || files[0].ID != stored.ID || files[0].Name != "persisted.txt" {
		t.Fatalf("Files() after the restart = %+v, want the stored attachment", files)
	}
}

func TestLoadDropsInvalidAttachmentMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ghostboard.json")

	doc := `{"boards":[{"name":"team","text":"hi","revision":1,"files":[` +
		`{"id":"not-an-id","name":"bad","size":1},` +
		`{"id":"mfrggzdfmztwq2lknnwg23tpob","name":"good.txt","size":0},` +
		`{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaa","name":"ok.txt","size":12,"content_type":"text/plain"}]}]}`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("writing the document: %v", err)
	}

	store, err := New(Config{
		FilePath:        path,
		ClearDelay:      time.Hour,
		MaxTextBytes:    1024,
		PersistInterval: time.Second,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	files, err := store.Files("team")
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}
	if len(files) != 1 || files[0].Name != "ok.txt" {
		t.Fatalf("Files() = %+v, want only the valid entry", files)
	}
}

func TestReconcileFilesAlignsDiskAndMetadata(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		FilePath:        filepath.Join(dir, "ghostboard.json"),
		ClearDelay:      time.Hour,
		MaxTextBytes:    1024,
		PersistInterval: time.Second,
		Files: FileConfig{
			Enabled:         true,
			Root:            filepath.Join(dir, "files"),
			MaxFileBytes:    1 << 20,
			MaxBoardBytes:   1 << 20,
			MaxTotalBytes:   1 << 20,
			MaxFileCount:    5,
			TTL:             time.Hour,
			TransferTimeout: time.Minute,
		},
	}

	store, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	kept, err := store.WriteFile(context.Background(), "team", "kept.txt", "", strings.NewReader("kept"))
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	missing, err := store.WriteFile(context.Background(), "team", "missing.txt", "", strings.NewReader("gone"))
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	boardDir := filepath.Join(cfg.Files.Root, "team")
	orphan := filepath.Join(boardDir, "b2zhitv7dbt5x2alykgfxb375tu")
	if err := os.WriteFile(orphan, []byte("orphan"), 0o600); err != nil {
		t.Fatalf("writing the orphan: %v", err)
	}
	temp := filepath.Join(boardDir, fileTempPrefix+"leftover")
	if err := os.WriteFile(temp, []byte("partial"), 0o600); err != nil {
		t.Fatalf("writing the leftover: %v", err)
	}
	if err := os.Mkdir(filepath.Join(boardDir, "subdir"), 0o755); err != nil {
		t.Fatalf("creating the directory: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatalf("writing the outside file: %v", err)
	}
	link := filepath.Join(boardDir, "c2zhitv7dbt5x2alykgfxb375tu")
	if err := os.Symlink(outside, link); err != nil {
		t.Logf("symlinks are not available here: %v", err)
	}
	if err := os.Remove(filepath.Join(boardDir, missing.ID)); err != nil {
		t.Fatalf("removing the file with metadata: %v", err)
	}

	unknownDir := filepath.Join(cfg.Files.Root, "ghost")
	if err := os.MkdirAll(unknownDir, 0o755); err != nil {
		t.Fatalf("creating the unknown board directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(unknownDir, "x"), []byte("x"), 0o600); err != nil {
		t.Fatalf("writing into the unknown directory: %v", err)
	}

	reopened, err := New(cfg)
	if err != nil {
		t.Fatalf("reopening the store: %v", err)
	}

	files, err := reopened.Files("team")
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}
	if len(files) != 1 || files[0].ID != kept.ID {
		t.Fatalf("Files() = %+v, want only the file that still exists", files)
	}

	for name, path := range map[string]string{
		"orphan":        orphan,
		"leftover":      temp,
		"directory":     filepath.Join(boardDir, "subdir"),
		"symlink":       link,
		"unknown board": unknownDir,
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still exists at %s", name, path)
		}
	}
	if _, err := os.Stat(filepath.Join(boardDir, kept.ID)); err != nil {
		t.Errorf("the kept file was removed: %v", err)
	}
}

func TestSanitizeFileName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{name: "plain", input: "shot.png", want: "shot.png", ok: true},
		{name: "windows path", input: `C:\Users\me\shot.png`, want: "shot.png", ok: true},
		{name: "unix path", input: "../../etc/passwd", want: "passwd", ok: true},
		{name: "control characters", input: "a\x00b\tc\nd", want: "abcd", ok: true},
		{name: "surrounding spaces", input: "  note.txt  ", want: "note.txt", ok: true},
		{name: "empty", input: "", ok: false},
		{name: "spaces only", input: "   ", ok: false},
		{name: "dot dot", input: "..", ok: false},
		{name: "trailing slash", input: "dir/", want: "dir", ok: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SanitizeFileName(tt.input)
			if tt.ok {
				if err != nil {
					t.Fatalf("SanitizeFileName(%q) error = %v", tt.input, err)
				}
				if got != tt.want {
					t.Errorf("SanitizeFileName(%q) = %q, want %q", tt.input, got, tt.want)
				}
				return
			}
			if !errors.Is(err, ErrInvalidFileName) {
				t.Errorf("SanitizeFileName(%q) error = %v, want ErrInvalidFileName", tt.input, err)
			}
		})
	}
}

func TestSanitizeFileNameBoundsTheLength(t *testing.T) {
	name := strings.Repeat("è", 300)
	got, err := SanitizeFileName(name)
	if err != nil {
		t.Fatalf("SanitizeFileName() error = %v", err)
	}
	if utf8.RuneCountInString(got) != fileNameMaxRunes {
		t.Errorf("length = %d runes, want %d", utf8.RuneCountInString(got), fileNameMaxRunes)
	}
}

func TestFileLimitsAreValidated(t *testing.T) {
	dir := t.TempDir()
	base := Config{
		FilePath:        filepath.Join(dir, "ghostboard.json"),
		ClearDelay:      time.Hour,
		MaxTextBytes:    1024,
		PersistInterval: time.Second,
		Files: FileConfig{
			Enabled:         true,
			Root:            filepath.Join(dir, "files"),
			MaxFileBytes:    1,
			MaxBoardBytes:   2,
			MaxTotalBytes:   3,
			MaxFileCount:    1,
			TTL:             time.Hour,
			TransferTimeout: time.Minute,
		},
	}

	tests := []struct {
		mutate func(*FileConfig)
		name   string
	}{
		{name: "missing root", mutate: func(f *FileConfig) { f.Root = "" }},
		{name: "max file bytes", mutate: func(f *FileConfig) { f.MaxFileBytes = 0 }},
		{name: "max file count", mutate: func(f *FileConfig) { f.MaxFileCount = 0 }},
		{name: "max board bytes", mutate: func(f *FileConfig) { f.MaxBoardBytes = 0 }},
		{name: "max total bytes", mutate: func(f *FileConfig) { f.MaxTotalBytes = 0 }},
		{name: "ttl", mutate: func(f *FileConfig) { f.TTL = 0 }},
		{name: "transfer timeout", mutate: func(f *FileConfig) { f.TransferTimeout = 0 }},
		{name: "decreasing limits", mutate: func(f *FileConfig) { f.MaxFileBytes = 10 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			cfg.Files = base.Files
			tt.mutate(&cfg.Files)
			if _, err := New(cfg); err == nil {
				t.Fatal("New() accepted an invalid file configuration")
			}
		})
	}
}

func TestFileLimitsJSONShape(t *testing.T) {
	store := newFileTestStore(t, nil)

	file, err := store.WriteFile(context.Background(), "team", "a.txt", "", strings.NewReader("a"))
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	data, err := json.Marshal(file)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	for _, key := range []string{`"id"`, `"name"`, `"size"`, `"content_type"`, `"created_at"`, `"expires_at"`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("JSON %s does not carry %s", data, key)
		}
	}
}

// assertBoardDirEmpty reports whether a board directory holds nothing, neither
// a committed file nor a leftover of a rejected upload.
func assertBoardDirEmpty(t *testing.T, store *Store, board string) {
	t.Helper()

	dir := filepath.Join(store.cfg.Files.Root, board)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		t.Fatalf("reading %s: %v", dir, err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("board directory %s still holds %v", dir, names)
	}
}
