package app

import (
	"context"
	"errors"
	"time"

	"github.com/asgambat/gospin/internal/cache"
	"github.com/asgambat/gospin/internal/config"
	"github.com/asgambat/gospin/internal/ghostboard"
	"github.com/asgambat/gospin/internal/logger"
	"github.com/asgambat/gospin/internal/repository"
	"github.com/asgambat/gospin/internal/runtime"
	"github.com/asgambat/gospin/internal/scheduler"
)

// App is the application container (immutable dependencies + lifecycle context).
// It is not a request context; handlers should still use gin's request context.
type App struct {
	Config     *config.Config
	Repo       repository.Repository
	Cache      cache.AppStore
	Runtime    runtime.ContainerRuntime
	Ghostboard *ghostboard.Store
	// GhostboardAuth is the PIN gate of the private boards, nil when no PIN is
	// configured and every board is public.
	GhostboardAuth *ghostboard.Authenticator

	BaseCtx     context.Context
	Cancel      context.CancelFunc
	persistDone <-chan struct{} // signal for completion of persistence scheduler
}

func New(cfg *config.Config, repo repository.Repository, store cache.AppStore, rt runtime.ContainerRuntime) (*App, error) {
	logger.WithComponent("app").Debugf("initializing app container")

	if cfg == nil {
		logger.WithComponent("app").Errorf("config is nil")
		return nil, errors.New("config is nil")
	}
	if repo == nil {
		logger.WithComponent("app").Errorf("repo is nil")
		return nil, errors.New("repo is nil")
	}
	if store == nil {
		logger.WithComponent("app").Errorf("cache store is nil")
		return nil, errors.New("cache store is nil")
	}
	if rt == nil {
		logger.WithComponent("app").Errorf("runtime is nil")
		return nil, errors.New("runtime is nil")
	}

	logger.WithComponent("app").Debugf("all dependencies validated")

	ghostboardCfg := cfg.Ghostboard.WithDefaults()
	ghostboardStore, err := ghostboard.New(ghostboard.Config{
		FilePath:        ghostboardCfg.FilePath,
		ClearDelay:      ghostboardCfg.ClearDelay,
		MaxTextBytes:    ghostboardCfg.MaxTextBytes,
		PersistInterval: ghostboardCfg.PersistInterval,
		Files: ghostboard.FileConfig{
			// Attachments live next to the persistence file; without one there
			// is no place to reconcile them at startup, so they stay off.
			Enabled:         ghostboardCfg.FilesEnabled && ghostboardCfg.FilesRoot() != "",
			Root:            ghostboardCfg.FilesRoot(),
			MaxFileBytes:    ghostboardCfg.MaxFileBytes,
			MaxBoardBytes:   ghostboardCfg.MaxBoardFileBytes,
			MaxTotalBytes:   ghostboardCfg.MaxTotalFileBytes,
			MaxFileCount:    ghostboardCfg.MaxFileCount,
			TTL:             ghostboardCfg.FileTTL,
			TransferTimeout: ghostboardCfg.FileTransferTimeout,
		},
	})
	if err != nil {
		logger.WithComponent("app").Errorf("cannot initialize ghostboard store: %v", err)
		return nil, err
	}

	var ghostboardAuth *ghostboard.Authenticator
	if ghostboardCfg.PINHMAC != "" {
		ghostboardAuth, err = ghostboard.NewAuthenticator(ghostboard.AuthConfig{
			PINDigest:     ghostboardCfg.PINHMAC,
			Secret:        ghostboardCfg.PINSecret,
			PrivateBoards: ghostboardCfg.PrivateBoards,
			SessionTTL:    ghostboardCfg.SessionTTL,
			MaxAttempts:   ghostboardCfg.PINMaxAttempts,
			Lockout:       ghostboardCfg.PINLockout,
		})
		if err != nil {
			logger.WithComponent("app").Errorf("cannot initialize the ghostboard pin gate: %v", err)
			return nil, err
		}
		logger.WithComponent("app").Debugf("ghostboard pin gate enabled for %d private board(s)", len(ghostboardCfg.PrivateBoards))
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &App{
		Config:         cfg,
		Repo:           repo,
		Cache:          store,
		Runtime:        rt,
		Ghostboard:     ghostboardStore,
		GhostboardAuth: ghostboardAuth,
		BaseCtx:        ctx,
		Cancel:         cancel,
	}, nil
}

func (a *App) Shutdown() {
	logger.WithComponent("app").Debugf("shutting down app container")

	if a == nil || a.Cancel == nil {
		logger.WithComponent("app").Debugf("app or cancel is nil, skipping shutdown")
		return
	}
	a.Cancel()

	// Flush ghostboard changes synchronously: the persistence goroutine may be
	// cancelled before its final write completes.
	if a.Ghostboard != nil {
		if err := a.Ghostboard.Flush(); err != nil {
			logger.WithComponent("app").Errorf("ghostboard flush on shutdown failed: %v", err)
		}
	}

	// Attende il completamento del persistence scheduler
	if a.persistDone != nil {
		logger.WithComponent("app").Debugf("waiting for persistence scheduler to complete")
		<-a.persistDone
	}

	logger.WithComponent("app").Debugf("app shutdown completed")
}

func (a *App) StartWatchers() {
	logger.WithComponent("app").Debugf("starting watchers")

	if err := a.Repo.StartWatcher(a.BaseCtx, a.Cache); err != nil {
		logger.WithComponent("app").Fatalf("cannot start config file watcher: %v", err)
	}

	logger.WithComponent("app").Debugf("file watcher started")

	// Start scheduled persistence goroutine
	a.persistDone = cache.StartPersistenceScheduler(a.BaseCtx, a.Cache, a.Repo, a.Config.Data.PersistInterval)
	logger.WithComponent("app").Debugf("persistence scheduler started")

	if a.Ghostboard != nil {
		a.Ghostboard.StartPersistence(a.BaseCtx)
		logger.WithComponent("app").Debugf("ghostboard persistence started")
	}

	if a.Config.Data.SchedulingEnabled {
		loc := time.Local
		if a.Config.Misc.SchedulingTZ != "" && a.Config.Misc.SchedulingTZ != "Local" {
			l, err := time.LoadLocation(a.Config.Misc.SchedulingTZ)
			if err != nil {
				logger.WithComponent("app").Fatalf("invalid scheduling timezone: %v", err)
			}
			loc = l
		}

		logger.WithComponent("app").Debugf("starting polling scheduler with timezone: %v", loc)
		s := scheduler.NewPollingScheduler(a.Cache, a.Runtime, a.Config.Data.SchedulingPoll, loc)
		s.Start(a.BaseCtx)
	}

	logger.WithComponent("app").Debugf("all watchers started successfully")
}
