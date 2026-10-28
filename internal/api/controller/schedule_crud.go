package controller

import (
	"fmt"

	"github.com/go-playground/validator/v10"

	"github.com/asgambat/gospin/internal/cache"
	"github.com/asgambat/gospin/internal/repository"
)

// ScheduleCrudService implements CrudService for schedules.
type ScheduleCrudService struct {
	Store cache.ScheduleStore
}

func (s *ScheduleCrudService) All() ([]repository.Schedule, error) {
	doc, err := s.Store.Snapshot()
	if err != nil {
		return nil, err
	}
	return doc.Schedules, nil
}

func (s *ScheduleCrudService) Add(item repository.Schedule) ([]repository.Schedule, error) {
	doc, err := s.Store.AddSchedule(item)
	if err != nil {
		return nil, err
	}
	return doc.Schedules, nil
}

func (s *ScheduleCrudService) Remove(id string) ([]repository.Schedule, error) {
	doc, err := s.Store.RemoveSchedule(id)
	if err != nil {
		return nil, err
	}
	return doc.Schedules, nil
}

// ScheduleCrudValidator implements CrudValidator for schedules.
type ScheduleCrudValidator struct {
	validator *validator.Validate
	store     cache.ScheduleStore
}

// Validate checks the schedule payload: struct-level rules (required fields,
// target type, HH:MM times), the timer contract (at least one of
// StartTime/StopTime must be provided) and the existence of the target. The
// last check matters because the data loader prunes schedules whose target no
// longer exists: accepting an orphan here would persist a schedule that
// silently disappears on the next reload.
func (v *ScheduleCrudValidator) Validate(item repository.Schedule) error {
	if err := v.validator.Struct(item); err != nil {
		return err
	}
	for i, timer := range item.Timers {
		if timer.StartTime == "" && timer.StopTime == "" {
			return fmt.Errorf("timer %d must define startTime, stopTime or both", i)
		}
	}
	return v.validateTargetExists(item)
}

// validateTargetExists rejects schedules whose target container or group is not
// part of the current data document.
func (v *ScheduleCrudValidator) validateTargetExists(item repository.Schedule) error {
	if v.store == nil {
		return nil
	}

	doc, err := v.store.Snapshot()
	if err != nil {
		return fmt.Errorf("cannot verify schedule target: %w", err)
	}

	switch item.TargetType {
	case "container":
		for _, c := range doc.Containers {
			if c.Name == item.Target {
				return nil
			}
		}
		return fmt.Errorf("target container %q not found", item.Target)
	case "group":
		for _, g := range doc.Groups {
			if g.Name == item.Target {
				return nil
			}
		}
		return fmt.Errorf("target group %q not found", item.Target)
	}

	return nil
}
