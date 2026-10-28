package cache

import "github.com/asgambat/gospin/internal/repository"

// ReadOnlyStore is the minimal cache API for read-only controllers.
type ReadOnlyStore interface {
	Snapshot() (repository.DataDocument, error)
}

// ContainerStore is the cache API needed by container handlers.
type ContainerStore interface {
	ReadOnlyStore
	AddContainer(container repository.Container) (repository.DataDocument, error)
	RemoveContainer(name string) (repository.DataDocument, error)
}

// GroupStore is the cache API needed by group handlers.
type GroupStore interface {
	ReadOnlyStore
	AddGroup(group repository.Group) (repository.DataDocument, error)
	RemoveGroup(name string) (repository.DataDocument, error)
}

// ScheduleStore is the cache API needed by schedule handlers.
type ScheduleStore interface {
	ReadOnlyStore
	AddSchedule(schedule repository.Schedule) (repository.DataDocument, error)
	RemoveSchedule(id string) (repository.DataDocument, error)
}

// PersistableStore is the cache API needed by the persistence scheduler.
type PersistableStore interface {
	IsDirty() bool
	Snapshot() (repository.DataDocument, error)
	ClearDirty()
	SetLastUpdate(ts int64)
}

// AppStore is the cache contract the application container exposes.
// It is intentionally broad: it supports controllers, persistence scheduler and repository watcher.
type AppStore interface {
	repository.CacheStore
	ContainerStore
	GroupStore
	ScheduleStore
	PersistableStore
}
