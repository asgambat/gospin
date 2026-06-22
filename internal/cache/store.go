package cache

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"github.com/bassista/go_spin/internal/logger"
	"github.com/bassista/go_spin/internal/repository"
)

var ErrContainerNotFound = errors.New("container not found")
var ErrGroupNotFound = errors.New("group not found")
var ErrScheduleNotFound = errors.New("schedule not found")

// Store keeps an in-memory copy of the data document.
type Store struct {
	data       repository.DataDocument
	lastUpdate int64
	mu         sync.RWMutex
	dirty      bool
}

// NewStore creates an empty cache store.
func NewStore(doc repository.DataDocument) *Store {
	return &Store{data: doc, lastUpdate: doc.Metadata.LastUpdate}
}

// MarkDirty sets the dirty flag to true.
func (s *Store) MarkDirty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dirty = true
}

// IsDirty returns true if cache has uncommitted changes.
func (s *Store) IsDirty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dirty
}

// ClearDirty resets the dirty flag.
func (s *Store) ClearDirty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dirty = false
}

// GetLastUpdate returns the cache's last update timestamp.
func (s *Store) GetLastUpdate() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastUpdate
}

// SetLastUpdate sets the cache's last update timestamp.
func (s *Store) SetLastUpdate(ts int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastUpdate = ts
}

// Snapshot returns a deep copy of the cached data.
func (s *Store) Snapshot() (repository.DataDocument, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneData(s.data)
}

// Replace swaps the cached data.
func (s *Store) Replace(doc repository.DataDocument) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cloned, err := cloneData(doc)
	if err != nil {
		return err
	}
	s.data = cloned
	s.lastUpdate = doc.Metadata.LastUpdate
	s.dirty = false

	return nil
}

// AddContainer upserts a container by name, updating order and returning the new snapshot.
func (s *Store) AddContainer(container repository.Container) (repository.DataDocument, error) {
	logger.WithComponent("cache").Debugf("adding/updating container: %s", container.Name)
	s.mu.Lock()
	defer s.mu.Unlock()

	clonedContainer, err := cloneContainer(container)
	if err != nil {
		return repository.DataDocument{}, err
	}

	// Normalize FriendlyName to lowercase for consistency
	clonedContainer.FriendlyName = strings.ToLower(clonedContainer.FriendlyName)

	inOrder := false
	for _, name := range s.data.Order {
		if name == clonedContainer.Name {
			inOrder = true
			break
		}
	}

	replaced := false
	for i := range s.data.Containers {
		if s.data.Containers[i].Name == clonedContainer.Name {
			s.data.Containers[i] = clonedContainer
			replaced = true
			break
		}
	}

	if !replaced {
		s.data.Containers = append(s.data.Containers, clonedContainer)
	}

	if !inOrder {
		s.data.Order = append(s.data.Order, clonedContainer.Name)
	}

	// Mark cache as dirty after mutation
	s.dirty = true

	return cloneData(s.data)
}

// RemoveContainer deletes a container by name and removes it from the order list.
func (s *Store) RemoveContainer(name string) (repository.DataDocument, error) {
	logger.WithComponent("cache").Debugf("removing container: %s", name)
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i := range s.data.Containers {
		if s.data.Containers[i].Name == name {
			idx = i
			break
		}
	}
	if idx == -1 {
		return repository.DataDocument{}, ErrContainerNotFound
	}

	// Remove from Containers slice
	s.data.Containers = append(s.data.Containers[:idx], s.data.Containers[idx+1:]...)

	// Remove from Order slice
	for i := 0; i < len(s.data.Order); i++ {
		if s.data.Order[i] == name {
			s.data.Order = append(s.data.Order[:i], s.data.Order[i+1:]...)
			i--
		}
	}

	// Mark cache as dirty after mutation
	s.dirty = true

	// Remove schedules that target this container
	newSchedules := make([]repository.Schedule, 0, len(s.data.Schedules))
	for _, sch := range s.data.Schedules {
		if sch.TargetType == "container" && sch.Target == name {
			logger.WithComponent("cache").Debugf("removing schedule %s because it targets deleted container %s", sch.ID, name)
			continue
		}
		newSchedules = append(newSchedules, sch)
	}
	s.data.Schedules = newSchedules

	// Remove container references from all groups
	for gi := range s.data.Groups {
		newContainers := make([]string, 0, len(s.data.Groups[gi].Container))
		for _, cname := range s.data.Groups[gi].Container {
			if cname == name {
				logger.WithComponent("cache").Debugf("removing container %s from group %s", name, s.data.Groups[gi].Name)
				continue
			}
			newContainers = append(newContainers, cname)
		}
		s.data.Groups[gi].Container = newContainers
	}

	return cloneData(s.data)
}

// AddGroup upserts a group by name, updating group order and returning the new snapshot.
func (s *Store) AddGroup(group repository.Group) (repository.DataDocument, error) {
	logger.WithComponent("cache").Debugf("adding/updating group: %s with %d containers", group.Name, len(group.Container))
	s.mu.Lock()
	defer s.mu.Unlock()

	clonedGroup, err := cloneGroup(group)
	if err != nil {
		return repository.DataDocument{}, err
	}

	inOrder := false
	for _, name := range s.data.GroupOrder {
		if name == clonedGroup.Name {
			inOrder = true
			break
		}
	}

	replaced := false
	for i := range s.data.Groups {
		if s.data.Groups[i].Name == clonedGroup.Name {
			s.data.Groups[i] = clonedGroup
			replaced = true
			break
		}
	}

	if !replaced {
		s.data.Groups = append(s.data.Groups, clonedGroup)
	}

	if !inOrder {
		s.data.GroupOrder = append(s.data.GroupOrder, clonedGroup.Name)
	}

	// Mark cache as dirty after mutation
	s.dirty = true

	return cloneData(s.data)
}

// RemoveGroup deletes a group by name and removes it from the group order list.
func (s *Store) RemoveGroup(name string) (repository.DataDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i := range s.data.Groups {
		if s.data.Groups[i].Name == name {
			idx = i
			break
		}
	}
	if idx == -1 {
		return repository.DataDocument{}, ErrGroupNotFound
	}

	// Remove from Groups slice
	s.data.Groups = append(s.data.Groups[:idx], s.data.Groups[idx+1:]...)

	// Remove from GroupOrder slice
	for i := 0; i < len(s.data.GroupOrder); i++ {
		if s.data.GroupOrder[i] == name {
			s.data.GroupOrder = append(s.data.GroupOrder[:i], s.data.GroupOrder[i+1:]...)
			i--
		}
	}

	// Mark cache as dirty after mutation
	s.dirty = true

	// Remove schedules that target this group
	newSchedules := make([]repository.Schedule, 0, len(s.data.Schedules))
	for _, sch := range s.data.Schedules {
		if sch.TargetType == "group" && sch.Target == name {
			logger.WithComponent("cache").Debugf("removing schedule %s because it targets deleted group %s", sch.ID, name)
			continue
		}
		newSchedules = append(newSchedules, sch)
	}
	s.data.Schedules = newSchedules

	return cloneData(s.data)
}

// AddSchedule upserts a schedule by id and returns the new snapshot.
func (s *Store) AddSchedule(schedule repository.Schedule) (repository.DataDocument, error) {
	logger.WithComponent("cache").Debugf("adding/updating schedule: %s (target: %s, %d timers)", schedule.ID, schedule.Target, len(schedule.Timers))
	s.mu.Lock()
	defer s.mu.Unlock()

	clonedSchedule, err := cloneSchedule(schedule)
	if err != nil {
		return repository.DataDocument{}, err
	}

	replaced := false
	for i := range s.data.Schedules {
		if s.data.Schedules[i].ID == clonedSchedule.ID {
			s.data.Schedules[i] = clonedSchedule
			replaced = true
			break
		}
	}

	if !replaced {
		s.data.Schedules = append(s.data.Schedules, clonedSchedule)
	}

	// Mark cache as dirty after mutation
	s.dirty = true

	return cloneData(s.data)
}

// RemoveSchedule deletes a schedule by id.
func (s *Store) RemoveSchedule(id string) (repository.DataDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i := range s.data.Schedules {
		if s.data.Schedules[i].ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return repository.DataDocument{}, ErrScheduleNotFound
	}

	// Remove from Schedules slice
	s.data.Schedules = append(s.data.Schedules[:idx], s.data.Schedules[idx+1:]...)

	// Mark cache as dirty after mutation
	s.dirty = true

	return cloneData(s.data)
}

// cloneData deep-copies the document to avoid shared slices between cache and callers.
func cloneData(doc repository.DataDocument) (repository.DataDocument, error) {
	bytes, err := json.Marshal(doc)
	if err != nil {
		return repository.DataDocument{}, err
	}
	var copy repository.DataDocument
	if err := json.Unmarshal(bytes, &copy); err != nil {
		return repository.DataDocument{}, err
	}
	return copy, nil
}

// cloneContainer deep-copies a container to avoid shared pointer fields.
func cloneContainer(c repository.Container) (repository.Container, error) {
	bytes, err := json.Marshal(c)
	if err != nil {
		return repository.Container{}, err
	}
	var copy repository.Container
	if err := json.Unmarshal(bytes, &copy); err != nil {
		return repository.Container{}, err
	}
	return copy, nil
}

// cloneGroup deep-copies a group to avoid shared slices/pointer fields.
func cloneGroup(g repository.Group) (repository.Group, error) {
	bytes, err := json.Marshal(g)
	if err != nil {
		return repository.Group{}, err
	}
	var copy repository.Group
	if err := json.Unmarshal(bytes, &copy); err != nil {
		return repository.Group{}, err
	}
	return copy, nil
}

// cloneSchedule deep-copies a schedule to avoid shared slices/pointer fields.
func cloneSchedule(s repository.Schedule) (repository.Schedule, error) {
	bytes, err := json.Marshal(s)
	if err != nil {
		return repository.Schedule{}, err
	}
	var copy repository.Schedule
	if err := json.Unmarshal(bytes, &copy); err != nil {
		return repository.Schedule{}, err
	}
	return copy, nil
}
