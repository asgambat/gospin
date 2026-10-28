package controller

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/asgambat/gospin/internal/logger"
	"github.com/asgambat/gospin/internal/repository"
)

// removeOrphansRequest carries the names the client wants to delete. The client
// is expected to send back exactly the list returned by GET /containers/orphans.
type removeOrphansRequest struct {
	Names []string `json:"names"`
}

// removeOrphansResponse reports what the server actually deleted after
// re-verifying each name against the runtime.
type removeOrphansResponse struct {
	Removed []string `json:"removed"`
	Skipped []string `json:"skipped"`
}

// ListOrphanContainers handles GET /containers/orphans.
// It compares the registry (cache) against the containers known to the runtime
// (docker ps -a for the Docker runtime) and returns the registry entries that no
// longer exist. It only reads state: nothing is deleted.
func (cc *ContainerController) ListOrphanContainers(c *gin.Context) {
	logger.WithComponent("container-controller").Debugf("GET /containers/orphans handler called")

	orphans, err := cc.orphanContainers(c.Request.Context())
	if err != nil {
		logger.WithComponent("container-controller").Errorf("list orphan containers: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to compare the registry with the container runtime"})
		return
	}

	logger.WithComponent("container-controller").Infof("orphan container check found %d missing containers", len(orphans))
	c.JSON(http.StatusOK, orphans)
}

// RemoveOrphanContainers handles POST /containers/orphans/remove.
// Every requested name is re-verified against the runtime before deletion, so a
// container that reappeared between preview and confirmation is skipped instead
// of being removed. Deleting a container also removes the schedules targeting
// it and its references inside groups.
func (cc *ContainerController) RemoveOrphanContainers(c *gin.Context) {
	logger.WithComponent("container-controller").Debugf("POST /containers/orphans/remove handler called")

	var req removeOrphansRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	names := dedupeNonEmpty(req.Names)
	if len(names) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no container names provided"})
		return
	}

	orphans, err := cc.orphanContainers(c.Request.Context())
	if err != nil {
		logger.WithComponent("container-controller").Errorf("remove orphan containers: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to compare the registry with the container runtime"})
		return
	}

	candidates := make(map[string]struct{}, len(orphans))
	for _, container := range orphans {
		candidates[container.Name] = struct{}{}
	}

	response := removeOrphansResponse{Removed: []string{}, Skipped: []string{}}
	for _, name := range names {
		if _, ok := candidates[name]; !ok {
			logger.WithComponent("container-controller").Warnf("skipping orphan removal for %s: container exists in the runtime or is not registered", name)
			response.Skipped = append(response.Skipped, name)
			continue
		}

		if _, err := cc.store.RemoveContainer(name); err != nil {
			logger.WithComponent("container-controller").Errorf("failed to remove orphan container %s: %v", name, err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   fmt.Sprintf("failed to remove container %q", name),
				"removed": response.Removed,
				"skipped": response.Skipped,
			})
			return
		}

		logger.WithComponent("container-controller").Infof("removed orphan container %s", name)
		response.Removed = append(response.Removed, name)
	}

	logger.WithComponent("container-controller").Infof("orphan cleanup removed %d containers and skipped %d", len(response.Removed), len(response.Skipped))
	c.JSON(http.StatusOK, response)
}

// orphanContainers returns the registry entries whose name is absent from the
// runtime. The registry order is preserved so the UI shows a stable list.
func (cc *ContainerController) orphanContainers(ctx context.Context) ([]repository.Container, error) {
	doc, err := cc.store.Snapshot()
	if err != nil {
		return nil, fmt.Errorf("snapshot registry: %w", err)
	}

	names, err := cc.runtime.ListContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list runtime containers: %w", err)
	}

	present := make(map[string]struct{}, len(names))
	for _, name := range names {
		present[name] = struct{}{}
	}

	orphans := make([]repository.Container, 0)
	for _, container := range doc.Containers {
		if _, ok := present[container.Name]; ok {
			continue
		}
		orphans = append(orphans, container)
	}

	return orphans, nil
}

// dedupeNonEmpty trims the input names, drops empty entries and removes duplicates
// while preserving the original order.
func dedupeNonEmpty(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
