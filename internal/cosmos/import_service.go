package cosmos

import (
	"context"
	"strings"

	"github.com/bassista/go_spin/internal/cache"
	"github.com/bassista/go_spin/internal/logger"
	"github.com/bassista/go_spin/internal/repository"
)

type ImportResult struct {
	TotalFetched    int      `json:"total_fetched"`
	Filtered        int      `json:"filtered"`
	Imported        int      `json:"imported"`
	SkippedExisting int      `json:"skipped_existing"`
	ImportedNames   []string `json:"imported_names"`
	SkippedNames    []string `json:"skipped_names"`
	Errors          []string `json:"errors,omitempty"`
}

type ImportService struct {
	client *Client
	store  cache.ContainerStore
}

func NewImportService(client *Client, store cache.ContainerStore) *ImportService {
	return &ImportService{
		client: client,
		store:  store,
	}
}

func (s *ImportService) ImportContainers(ctx context.Context, baseUrl, token string) (*ImportResult, error) {
	logger.WithComponent("cosmos-import").Info("starting container import from cosmos")

	routes, err := s.client.FetchRoutes(ctx, baseUrl, token)
	if err != nil {
		return nil, err
	}

	result := &ImportResult{
		TotalFetched:  len(routes),
		ImportedNames: []string{},
		SkippedNames:  []string{},
		Errors:        []string{},
	}

	validRoutes := FilterValidRoutes(routes)
	result.Filtered = len(validRoutes)

	existingContainers, err := s.getExistingContainerNames()
	if err != nil {
		return nil, err
	}

	for _, route := range validRoutes {
		containerName := extractContainerName(route.Target)
		if containerName == "" {
			logger.WithComponent("cosmos-import").Warnf("skipping route with invalid target: %s", route.Target)
			continue
		}

		if existingContainers[containerName] {
			logger.WithComponent("cosmos-import").Debugf("skipping existing container: %s", containerName)
			result.SkippedExisting++
			result.SkippedNames = append(result.SkippedNames, containerName)
			continue
		}

		container := s.routeToContainer(route)
		if _, err := s.store.AddContainer(container); err != nil {
			logger.WithComponent("cosmos-import").Warnf("failed to add container %s: %v", containerName, err)
			result.Errors = append(result.Errors, err.Error())
			continue
		}

		logger.WithComponent("cosmos-import").Infof("imported container: %s", containerName)
		result.Imported++
		result.ImportedNames = append(result.ImportedNames, containerName)
	}

	logger.WithComponent("cosmos-import").Infof("import completed: total=%d, filtered=%d, imported=%d, skipped=%d, errors=%d",
		result.TotalFetched, result.Filtered, result.Imported, result.SkippedExisting, len(result.Errors))

	return result, nil
}

func (s *ImportService) getExistingContainerNames() (map[string]bool, error) {
	doc, err := s.store.Snapshot()
	if err != nil {
		return nil, err
	}

	names := make(map[string]bool)
	for _, c := range doc.Containers {
		names[c.Name] = true
	}
	return names, nil
}

func (s *ImportService) routeToContainer(route RouteItem) repository.Container {
	url := route.Host
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}

	containerName := extractContainerName(route.Target)

	active := true
	favorite := false
	running := false

	return repository.Container{
		Name:         containerName,
		FriendlyName: route.Name,
		URL:          url,
		Active:       &active,
		Favorite:     &favorite,
		Running:      &running,
	}
}

func extractContainerName(target string) string {
	if target == "" {
		return ""
	}

	idx := strings.Index(target, "//")
	if idx == -1 {
		return ""
	}

	afterSlashSlash := target[idx+2:]
	endIdx := strings.Index(afterSlashSlash, ":")
	if endIdx == -1 {
		return afterSlashSlash
	}

	return afterSlashSlash[:endIdx]
}
