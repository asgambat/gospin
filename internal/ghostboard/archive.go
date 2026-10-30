package ghostboard

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/asgambat/gospin/internal/logger"
)

// Archive describes what ArchiveFiles wrote: how many attachments made it into
// the archive and how many bytes of them it carries.
type Archive struct {
	Files int
	Bytes int64
}

// ArchiveFiles streams every attachment of a board as one ZIP archive.
//
// Entries are stored, not deflated: an attachment is usually a screenshot, a
// PDF or another archive already, and the transfer the caller pays for anyway
// is the one cost worth optimising. The archive is built from the live store,
// so an attachment that disappears between the listing and its turn (the idle
// clear, a delete) is left out instead of failing the download: the client gets
// the files that are still there.
func (s *Store) ArchiveFiles(ctx context.Context, board string, w io.Writer) (Archive, error) {
	if err := ValidateBoardName(board); err != nil {
		return Archive{}, err
	}
	if !s.cfg.Files.Enabled {
		return Archive{}, ErrFilesDisabled
	}

	files, err := s.Files(board)
	if err != nil {
		return Archive{}, err
	}
	if len(files) == 0 {
		return Archive{}, ErrNoFiles
	}

	writer := zip.NewWriter(w)
	names := make(map[string]struct{}, len(files))

	var archive Archive
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return archive, err
		}

		_, handle, err := s.OpenFile(board, file.ID)
		if errors.Is(err, ErrFileNotFound) {
			logger.WithComponent("ghostboard").Debugf("leaving attachment %s of board %q out of its archive: it is gone", file.ID, board)
			continue
		}
		if err != nil {
			return archive, err
		}

		entry, err := writer.CreateHeader(&zip.FileHeader{
			Name:     archiveEntryName(file.Name, names),
			Method:   zip.Store,
			Modified: file.CreatedAt,
		})
		if err != nil {
			closeArchiveEntry(handle, board, file.ID)
			return archive, fmt.Errorf("adding attachment %s of board %q to its archive: %w", file.ID, board, err)
		}

		written, copyErr := io.Copy(entry, handle)
		closeArchiveEntry(handle, board, file.ID)
		if copyErr != nil {
			return archive, fmt.Errorf("writing attachment %s of board %q into its archive: %w", file.ID, board, copyErr)
		}

		archive.Files++
		archive.Bytes += written
	}

	if err := writer.Close(); err != nil {
		return archive, fmt.Errorf("closing the archive of board %q: %w", board, err)
	}
	if archive.Files == 0 {
		logger.WithComponent("ghostboard").Warnf("board %q lost every attachment while its archive was built", board)
	}
	return archive, nil
}

// archiveEntryName turns the label of an attachment into a unique entry name.
// Two files uploaded under the same name must not overwrite each other when the
// archive is extracted, so the second one becomes "name (2).ext".
func archiveEntryName(name string, used map[string]struct{}) string {
	label, err := SanitizeFileName(name)
	if err != nil {
		label = "file"
	}
	if _, taken := used[label]; !taken {
		used[label] = struct{}{}
		return label
	}

	ext := path.Ext(label)
	stem := strings.TrimSuffix(label, ext)
	if stem == "" {
		stem, ext = label, ""
	}
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, suffix, ext)
		if _, taken := used[candidate]; !taken {
			used[candidate] = struct{}{}
			return candidate
		}
	}
}

// closeArchiveEntry closes the handle of one attachment, reporting a failure
// that the bytes already written made harmless.
func closeArchiveEntry(handle io.Closer, board, id string) {
	if err := handle.Close(); err != nil {
		logger.WithComponent("ghostboard").Debugf("closing attachment %s of board %q: %v", id, board, err)
	}
}
