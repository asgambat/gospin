package ghostboard

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// readArchive opens the ZIP ArchiveFiles produced and returns its entries by
// name, failing the test when the bytes are not a valid archive.
func readArchive(t *testing.T, data []byte) map[string]string {
	t.Helper()

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("reading the archive: %v", err)
	}

	entries := make(map[string]string, len(reader.File))
	for _, file := range reader.File {
		if file.Method != zip.Store {
			t.Errorf("entry %q uses method %d, want zip.Store", file.Name, file.Method)
		}
		handle, err := file.Open()
		if err != nil {
			t.Fatalf("opening entry %q: %v", file.Name, err)
		}
		var content bytes.Buffer
		if _, err := content.ReadFrom(handle); err != nil {
			t.Fatalf("reading entry %q: %v", file.Name, err)
		}
		if err := handle.Close(); err != nil {
			t.Fatalf("closing entry %q: %v", file.Name, err)
		}
		entries[file.Name] = content.String()
	}
	return entries
}

func TestArchiveFilesWritesEveryAttachment(t *testing.T) {
	store := newFileTestStore(t, nil)

	first, err := store.WriteFile(context.Background(), "team", "note.txt", "", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	second, err := store.WriteFile(context.Background(), "team", "other.txt", "", strings.NewReader("world!"))
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var buffer bytes.Buffer
	archive, err := store.ArchiveFiles(context.Background(), "team", &buffer)
	if err != nil {
		t.Fatalf("ArchiveFiles() error = %v", err)
	}
	if archive.Files != 2 || archive.Bytes != 11 {
		t.Errorf("ArchiveFiles() = %+v, want 2 files and 11 bytes", archive)
	}

	entries := readArchive(t, buffer.Bytes())
	if len(entries) != 2 {
		t.Fatalf("archive holds %d entries, want 2", len(entries))
	}
	if entries["note.txt"] != "hello" || entries["other.txt"] != "world!" {
		t.Errorf("archive entries = %v, want both files by name", entries)
	}

	// The archive carries the creation time of each attachment, so an extracted
	// file does not look older or newer than the board says. The ZIP format
	// stores the time in two-second steps.
	reader, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatalf("reading the archive: %v", err)
	}
	created := map[string]time.Time{"note.txt": first.CreatedAt, "other.txt": second.CreatedAt}
	for _, entry := range reader.File {
		if entry.Modified.Sub(created[entry.Name]).Abs() >= 2*time.Second {
			t.Errorf("entry %q is dated %v, want the upload time %v", entry.Name, entry.Modified, created[entry.Name])
		}
	}
}

func TestArchiveFilesRenamesDuplicateNames(t *testing.T) {
	store := newFileTestStore(t, func(cfg *Config) {
		cfg.Files.MaxFileCount = 5
		cfg.Files.MaxBoardBytes = 8 << 20
	})

	// The collision is not only between the duplicates: an attachment already
	// called "note (2).txt" must not be overwritten by the rename either.
	for _, name := range []string{"note (2).txt", "note.txt", "note.txt"} {
		if _, err := store.WriteFile(context.Background(), "team", name, "", strings.NewReader(name)); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", name, err)
		}
	}

	var buffer bytes.Buffer
	archive, err := store.ArchiveFiles(context.Background(), "team", &buffer)
	if err != nil {
		t.Fatalf("ArchiveFiles() error = %v", err)
	}
	if archive.Files != 3 {
		t.Errorf("ArchiveFiles() wrote %d files, want 3", archive.Files)
	}

	entries := readArchive(t, buffer.Bytes())
	if len(entries) != 3 {
		t.Fatalf("archive holds %d entries, want 3: %v", len(entries), entries)
	}
	for name, content := range map[string]string{
		"note.txt":         "note.txt",
		"note (2).txt":     "note.txt",
		"note (2) (2).txt": "note (2).txt",
	} {
		if entries[name] != content {
			t.Errorf("entry %q = %q, want %q", name, entries[name], content)
		}
	}
}

func TestArchiveEntryName(t *testing.T) {
	check := func(t *testing.T, name, want string, taken ...string) {
		t.Helper()

		used := make(map[string]struct{}, len(taken))
		for _, takenName := range taken {
			used[takenName] = struct{}{}
		}
		if got := archiveEntryName(name, used); got != want {
			t.Errorf("archiveEntryName(%q) = %q, want %q", name, got, want)
		}
	}

	t.Run("plain name", func(t *testing.T) { check(t, "note.txt", "note.txt") })
	t.Run("path stripped", func(t *testing.T) { check(t, "../../etc/passwd", "passwd") })
	t.Run("empty name", func(t *testing.T) { check(t, "", "file") })
	t.Run("second copy", func(t *testing.T) { check(t, "note.txt", "note (2).txt", "note.txt") })
	t.Run("third copy", func(t *testing.T) { check(t, "note.txt", "note (3).txt", "note.txt", "note (2).txt") })
	t.Run("leftover prefix", func(t *testing.T) { check(t, "note (2).txt", "note (2) (2).txt", "note (2).txt") })
	t.Run("extensionless", func(t *testing.T) { check(t, "notes", "notes (2)", "notes") })
	t.Run("dot file", func(t *testing.T) { check(t, ".env", ".env (2)", ".env") })
}

func TestArchiveFilesWithoutFiles(t *testing.T) {
	store := newFileTestStore(t, nil)

	var buffer bytes.Buffer
	if _, err := store.ArchiveFiles(context.Background(), "team", &buffer); !errors.Is(err, ErrNoFiles) {
		t.Fatalf("ArchiveFiles() error = %v, want ErrNoFiles", err)
	}
	if buffer.Len() != 0 {
		t.Errorf("ArchiveFiles() wrote %d byte(s) for an empty board", buffer.Len())
	}
}

func TestArchiveFilesDisabled(t *testing.T) {
	store := newTestStore(t)

	var buffer bytes.Buffer
	if _, err := store.ArchiveFiles(context.Background(), "team", &buffer); !errors.Is(err, ErrFilesDisabled) {
		t.Fatalf("ArchiveFiles() error = %v, want ErrFilesDisabled", err)
	}
}

func TestArchiveFilesRejectsAnInvalidBoard(t *testing.T) {
	store := newFileTestStore(t, nil)

	var buffer bytes.Buffer
	if _, err := store.ArchiveFiles(context.Background(), "../etc", &buffer); err == nil {
		t.Fatal("ArchiveFiles() with an invalid board name = nil error, want a failure")
	}
}

func TestArchiveFilesSkipsAVanishedAttachment(t *testing.T) {
	store := newFileTestStore(t, nil)

	gone, err := store.WriteFile(context.Background(), "team", "gone.txt", "", strings.NewReader("gone"))
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := store.WriteFile(context.Background(), "team", "kept.txt", "", strings.NewReader("kept")); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// The metadata still lists it, but the bytes are gone: the archive carries
	// what is left instead of failing the whole download.
	path := filepath.Join(store.cfg.Files.Root, "team", gone.ID)
	if err := os.Remove(path); err != nil {
		t.Fatalf("removing the stored bytes: %v", err)
	}

	var buffer bytes.Buffer
	archive, err := store.ArchiveFiles(context.Background(), "team", &buffer)
	if err != nil {
		t.Fatalf("ArchiveFiles() error = %v", err)
	}
	if archive.Files != 1 {
		t.Errorf("ArchiveFiles() wrote %d files, want 1", archive.Files)
	}

	entries := readArchive(t, buffer.Bytes())
	if len(entries) != 1 || entries["kept.txt"] != "kept" {
		t.Errorf("archive entries = %v, want only kept.txt", entries)
	}
}

func TestArchiveFilesHonoursCancellation(t *testing.T) {
	store := newFileTestStore(t, nil)

	if _, err := store.WriteFile(context.Background(), "team", "note.txt", "", strings.NewReader("hello")); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buffer bytes.Buffer
	if _, err := store.ArchiveFiles(ctx, "team", &buffer); !errors.Is(err, context.Canceled) {
		t.Fatalf("ArchiveFiles() error = %v, want context.Canceled", err)
	}
}
