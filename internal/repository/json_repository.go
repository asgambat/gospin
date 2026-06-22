package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-playground/validator/v10"

	"github.com/bassista/go_spin/internal/logger"
)

// CacheStore defines the interface for cache operations needed by the watcher callback.
type CacheStore interface {
	GetLastUpdate() int64
	IsDirty() bool
	Snapshot() (DataDocument, error)
	Replace(doc DataDocument) error
}

// JSONRepository handles disk persistence and watching of the data file.
type JSONRepository struct {
	validator *validator.Validate
	path      string
	dir       string
	base      string
	mu        sync.Mutex
}

// NewJSONRepository creates a repository for the given JSON file path.
// It returns the repository interface to avoid leaking implementation details.
func NewJSONRepository(path string) (Repository, error) {
	if path == "" {
		return nil, errors.New("data file path is required")
	}

	dir := filepath.Dir(path)
	base := filepath.Base(path)
	if dir == "" || dir == "." {
		dir = "."
	}

	v := validator.New()
	return &JSONRepository{path: path, dir: dir, base: base, validator: v}, nil
}

// Load reads the JSON file, parses and validates it.
// It respects context cancellation before performing I/O operations.
func (r *JSONRepository) Load(ctx context.Context) (*DataDocument, error) {
	logger.WithComponent("json-repo").Tracef("loading data from: %s", r.path)

	// Check for context cancellation before acquiring lock
	if err := ctx.Err(); err != nil {
		logger.WithComponent("json-repo").Warnf("load cancelled: %v", err)
		return nil, fmt.Errorf("load cancelled: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check again after acquiring lock
	if err := ctx.Err(); err != nil {
		logger.WithComponent("json-repo").Warnf("load cancelled: %v", err)
		return nil, fmt.Errorf("load cancelled: %w", err)
	}

	doc, err := r.loadUnlocked()
	if err != nil {
		logger.WithComponent("json-repo").Warnf("load failed: %v", err)
		return nil, err
	}
	logger.WithComponent("json-repo").Tracef("loaded data successfully, lastUpdate: %d, containers: %d, groups: %d, schedules: %d", doc.Metadata.LastUpdate, len(doc.Containers), len(doc.Groups), len(doc.Schedules))
	return doc, nil
}

// loadUnlocked reads the JSON file without acquiring the lock (caller must hold it).
func (r *JSONRepository) loadUnlocked() (*DataDocument, error) {
	file, err := os.Open(r.path)
	if err != nil {
		return nil, fmt.Errorf("open data file: %w", err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			logger.WithComponent("json-repo").Warnf("failed to close data file %s: %v", r.path, cerr)
		}
	}()

	var doc DataDocument
	if err := json.NewDecoder(file).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode data file: %w", err)
	}

	doc.ApplyDefaults()

	finalDoc := removeSchedulesWithMissingContainers(&doc)

	if r.validator != nil {
		if err := r.validator.Struct(finalDoc); err != nil {
			return nil, fmt.Errorf("validate data file: %w", err)
		}
	}

	return finalDoc, nil
}

func removeSchedulesWithMissingContainers(doc *DataDocument) *DataDocument {
	if doc == nil {
		return doc
	}

	// Build lookup sets for existing containers and groups
	containerSet := make(map[string]struct{}, len(doc.Containers))
	for _, c := range doc.Containers {
		containerSet[c.Name] = struct{}{}
	}
	groupSet := make(map[string]struct{}, len(doc.Groups))
	for _, g := range doc.Groups {
		groupSet[g.Name] = struct{}{}
	}

	filtered := make([]Schedule, 0, len(doc.Schedules))
	for _, s := range doc.Schedules {
		if s.TargetType == "container" {
			if _, ok := containerSet[s.Target]; !ok {
				logger.WithComponent("json-repo").Warnf("removing schedule %s: target container %s not found", s.ID, s.Target)
				continue
			}
		}
		if s.TargetType == "group" {
			if _, ok := groupSet[s.Target]; !ok {
				logger.WithComponent("json-repo").Warnf("removing schedule %s: target group %s not found", s.ID, s.Target)
				continue
			}
		}
		filtered = append(filtered, s)
	}

	doc.Schedules = filtered
	return doc
}

// Save validates and writes the document atomically to disk.
// It respects context cancellation before performing I/O operations.
func (r *JSONRepository) Save(ctx context.Context, doc *DataDocument) error {
	if doc == nil {
		logger.WithComponent("json-repo").Debugf("save failed: document is nil")
		return errors.New("document is nil")
	}

	// Check for context cancellation before validation
	if err := ctx.Err(); err != nil {
		logger.WithComponent("json-repo").Debugf("save cancelled: %v", err)
		return fmt.Errorf("save cancelled: %w", err)
	}

	logger.WithComponent("json-repo").Debugf("saving data to: %s (lastUpdate: %d)", r.path, doc.Metadata.LastUpdate)
	if r.validator != nil {
		if err := r.validator.Struct(doc); err != nil {
			logger.WithComponent("json-repo").Debugf("save failed: %v", err)
			return fmt.Errorf("validate before save: %w", err)
		}
	}

	// Check for context cancellation before acquiring lock
	if err := ctx.Err(); err != nil {
		logger.WithComponent("json-repo").Debugf("save cancelled: %v", err)
		return fmt.Errorf("save cancelled: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check again after acquiring lock
	if err := ctx.Err(); err != nil {
		logger.WithComponent("json-repo").Debugf("save cancelled: %v", err)
		return fmt.Errorf("save cancelled: %w", err)
	}

	if err := r.saveUnlocked(doc); err != nil {
		logger.WithComponent("json-repo").Debugf("save failed: %v", err)
		return err
	}
	logger.WithComponent("json-repo").Debugf("data saved successfully")
	return nil
}

// saveUnlocked writes the document without acquiring the lock (caller must hold it).
func (r *JSONRepository) saveUnlocked(doc *DataDocument) error {
	payload, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal data: %w", err)
	}

	tmpFile, err := os.CreateTemp(r.dir, r.base+".tmp-")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() {
		if cerr := tmpFile.Close(); cerr != nil && !errors.Is(cerr, os.ErrClosed) {
			logger.WithComponent("json-repo").Debugf("failed to close temp file %s: %v", tmpFile.Name(), cerr)
		}
		if rerr := os.Remove(tmpFile.Name()); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
			logger.WithComponent("json-repo").Tracef("failed to remove temp file %s: %v", tmpFile.Name(), rerr)
		}
	}()

	if _, err := tmpFile.Write(payload); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpFile.Name(), r.path); err != nil {
		return fmt.Errorf("replace data file: %w", err)
	}

	return nil
}

// StartWatcher listens for changes to the data file and calls onChange after debounce.
// It watches the parent directory (not the file) so atomic replace sequences (temp+rename)
// are still observed on Linux and Windows. Events are filtered by basename and
// debounced to avoid double reloads on write+chmod/rename cycles. The caller owns the
// provided context: cancel it to stop the goroutine and close the watcher cleanly.
func (r *JSONRepository) StartWatcher(ctx context.Context, cacheStore CacheStore) error {
	logger.WithComponent("json-repo").Debugf("starting file watcher for directory: %s", r.dir)
	onChange := r.MakeWatcherCallback(ctx, cacheStore)
	if onChange == nil {
		return errors.New("onChange callback is required")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.WithComponent("json-repo").Debugf("failed to create watcher: %v", err)
		return fmt.Errorf("create watcher: %w", err)
	}

	if err := watcher.Add(r.dir); err != nil {
		if cerr := watcher.Close(); cerr != nil {
			logger.WithComponent("json-repo").Debugf("failed to close watcher after Add error: %v", cerr)
		}
		logger.WithComponent("json-repo").Debugf("failed to watch directory: %v", err)
		return fmt.Errorf("watch dir: %w", err)
	}

	logger.WithComponent("json-repo").Debugf("file watcher started for: %s", r.dir)

	// Run watcher loop in the background; it exits when ctx is canceled or channels close.
	go func() {
		defer func() {
			if cerr := watcher.Close(); cerr != nil {
				logger.WithComponent("json-repo").Debugf("failed to close watcher: %v", cerr)
			}
		}()

		// debounce coalesces bursty fsnotify events (write+chmod/rename) into a single reload.
		// If the timer is stopped before it fires, the scheduled onChange will not run.
		var debounce *time.Timer
		schedule := func() {
			if debounce != nil {
				if !debounce.Stop() {
					select {
					case <-debounce.C:
					default:
					}
				}
			}
			debounce = time.AfterFunc(200*time.Millisecond, onChange)
		}

		for {
			select {
			case <-ctx.Done():
				logger.WithComponent("json-repo").Infof("file watcher shutting down")
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if filepath.Base(event.Name) != r.base {
					continue
				}
				logger.WithComponent("json-repo").Tracef("file event detected: %s (op: %v)", event.Name, event.Op)
				// Writes/Create/Chmod cover normal edits and atomic replace; trigger reload.
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Chmod) != 0 {
					schedule()
					continue
				}
				// Remove/Rename indicates the file was moved or replaced; wait for next Create.
				if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
					schedule()
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				logger.WithComponent("json-repo").Errorf("watcher error: %v", err)
			}
		}
	}()

	return nil
}

// MakeWatcherCallback returns a callback for file watcher that reloads cache from disk if needed.
// The provided ctx is used for the Load operation. Because the callback fires from a debounced
// timer after file events, it inherits the watcher context so cancellation propagates cleanly.
func (r *JSONRepository) MakeWatcherCallback(ctx context.Context, cacheStore CacheStore) func() {
	return func() {
		diskDoc, loadErr := r.Load(ctx)
		if loadErr != nil {
			logger.WithComponent("json-repo").Errorf("watch reload failed: %v", loadErr)
			return
		}
		cacheLastUpdate := cacheStore.GetLastUpdate()
		diskLastUpdate := diskDoc.Metadata.LastUpdate

		// If disk is not newer, skip reload
		if diskLastUpdate < cacheLastUpdate {
			logger.WithComponent("json-repo").Infof("disk version is not newer than cache: diskLastUpdate = %d, cacheLastUpdate = %d", diskLastUpdate, cacheLastUpdate)
			return
		}

		if cacheStore.IsDirty() {
			logger.WithComponent("json-repo").Warn("disk data is newer but cache is dirty; skipping reload")
			// the cache content will be written to file soon anyway
			return
		}

		isDiskSameAsCache := false
		if diskLastUpdate == cacheLastUpdate {
			// check if disk content is really the same as cache content
			snapshot, err := cacheStore.Snapshot()
			if err != nil {
				logger.WithComponent("json-repo").Errorf("cache reload error: failed to get snapshot: %v", err)
				return
			}
			isDiskSameAsCache = AreDataDocumentsEqual(&snapshot, diskDoc)
		}
		if !isDiskSameAsCache {
			if err := cacheStore.Replace(*diskDoc); err != nil {
				logger.WithComponent("json-repo").Errorf("cache reload error: %v", err)
				return
			}
			logger.WithComponent("json-repo").Info("cache reloaded from newer disk version")
		}
	}
}
