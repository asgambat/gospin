package controller

import (
	"github.com/go-playground/validator/v10"

	"github.com/bassista/go_spin/internal/cache"
	"github.com/bassista/go_spin/internal/repository"
)

// GroupCrudService implements CrudService for groups.
type GroupCrudService struct {
	Store cache.GroupStore
}

func (s *GroupCrudService) All() ([]repository.Group, error) {
	doc, err := s.Store.Snapshot()
	if err != nil {
		return nil, err
	}
	return sanitizeGroups(doc), nil
}

func (s *GroupCrudService) Add(item repository.Group) ([]repository.Group, error) {
	doc, err := s.Store.AddGroup(item)
	if err != nil {
		return nil, err
	}
	return sanitizeGroups(doc), nil
}

func (s *GroupCrudService) Remove(name string) ([]repository.Group, error) {
	doc, err := s.Store.RemoveGroup(name)
	if err != nil {
		return nil, err
	}
	return sanitizeGroups(doc), nil
}

// sanitizeGroups removes from each group any container names that are not
// present in the document's Containers list.
func sanitizeGroups(doc repository.DataDocument) []repository.Group {
	// Build set of existing container names
	containerSet := make(map[string]struct{}, len(doc.Containers))
	for _, c := range doc.Containers {
		containerSet[c.Name] = struct{}{}
	}

	sanitized := make([]repository.Group, 0, len(doc.Groups))
	for _, g := range doc.Groups {
		newContainers := make([]string, 0, len(g.Container))
		for _, cname := range g.Container {
			if _, ok := containerSet[cname]; ok {
				newContainers = append(newContainers, cname)
			}
		}
		g.Container = newContainers
		sanitized = append(sanitized, g)
	}
	return sanitized
}

// GroupCrudValidator implements CrudValidator for groups.
type GroupCrudValidator struct {
	validator *validator.Validate
}

func (v *GroupCrudValidator) Validate(item repository.Group) error {
	return v.validator.Struct(item)
}
