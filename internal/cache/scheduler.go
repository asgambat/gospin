package cache

import (
	"context"
	"time"

	"github.com/bassista/go_spin/internal/logger"
	"github.com/bassista/go_spin/internal/repository"
)

// StartPersistenceScheduler runs a goroutine that periodically flushes dirty cache to disk.
// On ctx.Done, it performs a final flush before returning.
// Returns a channel that is closed when the scheduler has completed shutdown.
func StartPersistenceScheduler(
	ctx context.Context,
	store PersistableStore,
	repo repository.Saver,
	interval time.Duration,
) <-chan struct{} {
	done := make(chan struct{})
	logger.WithComponent("persist").Debugf("starting persistence scheduler with interval: %v", interval)
	ticker := time.NewTicker(interval)
	go func() {
		defer close(done)
		defer ticker.Stop()
		logger.WithComponent("persist").Debugf("persistence scheduler running")
		for {
			select {
			case <-ctx.Done():
				logger.WithComponent("persist").Debugf("persistence scheduler received context cancellation, performing final flush")
				// Final flush on shutdown: preserve any context values (trace IDs, deadlines) from ctx
				// but detach its cancellation so the flush can complete even though the parent is cancelled.
				shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
				flushCache(shutdownCtx, store, repo)
				cancel()
				logger.WithComponent("persist").Info("persistence scheduler stopped after final flush")
				return
			case <-ticker.C:
				logger.WithComponent("persist").Tracef("persistence scheduler tick, checking if dirty")
				flushCache(ctx, store, repo)
			}
		}
	}()
	return done
}

// flushCache persists the cache to disk if dirty, using optimistic locking.
// It respects context cancellation to allow graceful shutdown.
func flushCache(ctx context.Context, store PersistableStore, repo repository.Saver) {
	if !store.IsDirty() {
		logger.WithComponent("persist").Tracef("cache is clean, skipping flush")
		return
	}

	// Check for context cancellation before proceeding
	if err := ctx.Err(); err != nil {
		logger.WithComponent("persist").Debugf("flush cancelled: %v", err)
		return
	}

	logger.WithComponent("persist").Debugf("cache is dirty, flushing to disk")
	// Cache is dirty → persist
	snapshot, err := store.Snapshot()
	if err != nil {
		logger.WithComponent("persist").Errorf("persist error: failed to get snapshot: %v", err)
		return
	}

	snapshot.Metadata.LastUpdate = time.Now().UnixMilli()

	if err := repo.Save(ctx, &snapshot); err != nil {
		logger.WithComponent("persist").Errorf("persist error: failed to save: %v", err)
		return
	}

	store.ClearDirty()
	store.SetLastUpdate(snapshot.Metadata.LastUpdate)
	logger.WithComponent("persist").Info("cache persisted to disk")
}
