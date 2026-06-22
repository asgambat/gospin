package controller

import (
	"github.com/go-playground/validator/v10"

	"github.com/bassista/go_spin/internal/cache"
	"github.com/bassista/go_spin/internal/repository"
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
}

func (v *ScheduleCrudValidator) Validate(item repository.Schedule) error {
	return v.validator.Struct(item)
}
