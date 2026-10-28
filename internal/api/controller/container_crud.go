package controller

import (
	"context"

	"github.com/asgambat/gospin/internal/cache"
	"github.com/asgambat/gospin/internal/repository"
	"github.com/asgambat/gospin/internal/runtime"

	"github.com/go-playground/validator/v10"
)

// ContainerCrudService implements CrudService for containers.
type ContainerCrudService struct {
	Store   cache.ContainerStore
	Runtime runtime.ContainerRuntime
	Ctx     context.Context
}

func (s *ContainerCrudService) All() ([]repository.Container, error) {
	doc, err := s.Store.Snapshot()
	if err != nil {
		return nil, err
	}

	for i := range doc.Containers {
		c := &doc.Containers[i]
		running, err := s.Runtime.IsRunning(s.Ctx, c.Name)
		if err != nil {
			falseVal := false
			c.Running = &falseVal
			continue
		}
		val := running
		c.Running = &val
	}

	return doc.Containers, nil
}

func (s *ContainerCrudService) Add(item repository.Container) ([]repository.Container, error) {
	doc, err := s.Store.AddContainer(item)
	if err != nil {
		return nil, err
	}
	return doc.Containers, nil
}

func (s *ContainerCrudService) Remove(name string) ([]repository.Container, error) {
	doc, err := s.Store.RemoveContainer(name)
	if err != nil {
		return nil, err
	}
	return doc.Containers, nil
}

// ContainerCrudValidator implements CrudValidator for containers.
type ContainerCrudValidator struct {
	validator *validator.Validate
}

func (v *ContainerCrudValidator) Validate(item repository.Container) error {
	return v.validator.Struct(item)
}
