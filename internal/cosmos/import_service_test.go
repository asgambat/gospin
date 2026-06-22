package cosmos

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bassista/go_spin/internal/cache"
	"github.com/bassista/go_spin/internal/repository"
)

type mockStore struct {
	addErr     error
	containers []repository.Container
}

func (m *mockStore) Snapshot() (repository.DataDocument, error) {
	return repository.DataDocument{Containers: m.containers}, nil
}

func (m *mockStore) AddContainer(c repository.Container) (repository.DataDocument, error) {
	if m.addErr != nil {
		return repository.DataDocument{}, m.addErr
	}
	m.containers = append(m.containers, c)
	return repository.DataDocument{Containers: m.containers}, nil
}

func (m *mockStore) RemoveContainer(name string) (repository.DataDocument, error) {
	return repository.DataDocument{}, nil
}

var _ cache.ContainerStore = (*mockStore)(nil)

func TestNewImportService(t *testing.T) {
	client := NewClient()
	store := &mockStore{}
	service := NewImportService(client, store)

	if service == nil {
		t.Fatal("expected service to be created")
	}
	if service.client == nil {
		t.Fatal("expected client to be set")
	}
	if service.store == nil {
		t.Fatal("expected store to be set")
	}
}

func TestImportContainers(t *testing.T) {
	tests := []struct {
		addErr             error
		name               string
		routes             []RouteItem
		existingContainers []repository.Container
		expectedImport     int
		expectedSkipped    int
		expectError        bool
	}{
		{
			name: "import new containers",
			routes: []RouteItem{
				{Disabled: false, Mode: "SERVAPP", Name: "Container 1", Target: "http://container1:8080", UseHost: true, Host: "host1.com"},
				{Disabled: false, Mode: "SERVAPP", Name: "Container 2", Target: "http://container2:8080", UseHost: true, Host: "host2.com"},
			},
			existingContainers: []repository.Container{},
			expectedImport:     2,
			expectedSkipped:    0,
			expectError:        false,
		},
		{
			name: "skip existing containers",
			routes: []RouteItem{
				{Disabled: false, Mode: "SERVAPP", Name: "Container 1", Target: "http://container1:8080", UseHost: true, Host: "host1.com"},
				{Disabled: false, Mode: "SERVAPP", Name: "Container 2", Target: "http://container2:8080", UseHost: true, Host: "host2.com"},
			},
			existingContainers: []repository.Container{
				{Name: "container1"},
			},
			expectedImport:  1,
			expectedSkipped: 1,
			expectError:     false,
		},
		{
			name:               "no routes to import",
			routes:             []RouteItem{},
			existingContainers: []repository.Container{},
			expectedImport:     0,
			expectedSkipped:    0,
			expectError:        false,
		},
		{
			name: "add error is logged but continues",
			routes: []RouteItem{
				{Disabled: false, Mode: "SERVAPP", Name: "Container 1", Target: "http://container1:8080", UseHost: true, Host: "host1.com"},
				{Disabled: false, Mode: "SERVAPP", Name: "Container 2", Target: "http://container2:8080", UseHost: true, Host: "host2.com"},
			},
			existingContainers: []repository.Container{},
			addErr:             errors.New("add failed"),
			expectedImport:     0,
			expectedSkipped:    0,
			expectError:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)

				data := make([]map[string]interface{}, 0, len(tt.routes))
				for _, r := range tt.routes {
					data = append(data, map[string]interface{}{
						"Disabled":      r.Disabled,
						"Mode":          r.Mode,
						"Name":          r.Name,
						"Target":        r.Target,
						"Description":   r.Description,
						"UseHost":       r.UseHost,
						"Host":          r.Host,
						"UsePathPrefix": r.UsePathPrefix,
						"PathPrefix":    r.PathPrefix,
					})
				}
				w.Write([]byte(`{"data":` + toJSON(data) + `,"status":"OK"}`))
			}))
			defer server.Close()

			store := &mockStore{
				containers: tt.existingContainers,
				addErr:     tt.addErr,
			}

			client := NewClient()
			service := NewImportService(client, store)

			result, err := service.ImportContainers(context.Background(), server.URL, "test-token")

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result.Imported != tt.expectedImport {
					t.Errorf("expected %d imported, got %d", tt.expectedImport, result.Imported)
				}
				if result.SkippedExisting != tt.expectedSkipped {
					t.Errorf("expected %d skipped, got %d", tt.expectedSkipped, result.SkippedExisting)
				}
			}
		})
	}
}

func TestRouteToContainer(t *testing.T) {
	tests := []struct {
		name           string
		expectedName   string
		expectedURL    string
		route          RouteItem
		expectActive   bool
		expectFavorite bool
	}{
		{
			name: "host without https prefix",
			route: RouteItem{
				Name:   "My Container",
				Target: "http://zmux:8096",
				Host:   "host.example.com",
			},
			expectedName:   "zmux",
			expectedURL:    "https://host.example.com",
			expectActive:   true,
			expectFavorite: false,
		},
		{
			name: "host with http prefix",
			route: RouteItem{
				Name:   "My Container",
				Target: "http://deluge:8112",
				Host:   "http://host.example.com",
			},
			expectedName:   "deluge",
			expectedURL:    "http://host.example.com",
			expectActive:   true,
			expectFavorite: false,
		},
		{
			name: "host with https prefix",
			route: RouteItem{
				Name:   "My Container",
				Target: "http://sonarr:7878",
				Host:   "https://host.example.com",
			},
			expectedName:   "sonarr",
			expectedURL:    "https://host.example.com",
			expectActive:   true,
			expectFavorite: false,
		},
		{
			name: "container with description",
			route: RouteItem{
				Name:        "My Container",
				Target:      "http://testcontainer:8080",
				Host:        "host.example.com",
				Description: "My Test Container",
			},
			expectedName:   "testcontainer",
			expectedURL:    "https://host.example.com",
			expectActive:   true,
			expectFavorite: false,
		},
		{
			name: "tcp protocol target",
			route: RouteItem{
				Name:   "Another Container",
				Target: "tcp://radarr:7878",
				Host:   "host.example.com",
			},
			expectedName:   "radarr",
			expectedURL:    "https://host.example.com",
			expectActive:   true,
			expectFavorite: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockStore{}
			client := NewClient()
			service := NewImportService(client, store)

			container := service.routeToContainer(tt.route)

			if container.Name != tt.expectedName {
				t.Errorf("expected name %s, got %s", tt.expectedName, container.Name)
			}
			if container.FriendlyName != tt.route.Name {
				t.Errorf("expected friendlyName %s, got %s", tt.route.Name, container.FriendlyName)
			}
			if container.URL != tt.expectedURL {
				t.Errorf("expected URL %s, got %s", tt.expectedURL, container.URL)
			}
			if container.Active == nil || *container.Active != tt.expectActive {
				t.Errorf("expected active=%v, got %v", tt.expectActive, container.Active)
			}
			if container.Favorite == nil || *container.Favorite != tt.expectFavorite {
				t.Errorf("expected favorite=%v, got %v", tt.expectFavorite, container.Favorite)
			}
		})
	}
}

func TestExtractContainerName(t *testing.T) {
	tests := []struct {
		target       string
		expectedName string
	}{
		{"http://zmux:8096", "zmux"},
		{"tcp://deluge:8112", "deluge"},
		{"https://sonarr:7878", "sonarr"},
		{"http://radarr:7878", "radarr"},
		{"", ""},
		{"invalid", ""},
		{"http://", ""},
		{"tcp://container", "container"},
		{"http://container:8080/path", "container"},
		{"http://container:8096/extra", "container"},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			result := extractContainerName(tt.target)
			if result != tt.expectedName {
				t.Errorf("extractContainerName(%s) = %s, want %s", tt.target, result, tt.expectedName)
			}
		})
	}
}

func TestImportResultFields(t *testing.T) {
	result := &ImportResult{
		TotalFetched:    10,
		Filtered:        5,
		Imported:        3,
		SkippedExisting: 2,
		ImportedNames:   []string{"c1", "c2", "c3"},
		SkippedNames:    []string{"c4", "c5"},
		Errors:          []string{"error1"},
	}

	if result.TotalFetched != 10 {
		t.Errorf("expected TotalFetched=10, got %d", result.TotalFetched)
	}
	if result.Filtered != 5 {
		t.Errorf("expected Filtered=5, got %d", result.Filtered)
	}
	if result.Imported != 3 {
		t.Errorf("expected Imported=3, got %d", result.Imported)
	}
	if result.SkippedExisting != 2 {
		t.Errorf("expected SkippedExisting=2, got %d", result.SkippedExisting)
	}
	if len(result.ImportedNames) != 3 {
		t.Errorf("expected 3 imported names, got %d", len(result.ImportedNames))
	}
	if len(result.SkippedNames) != 2 {
		t.Errorf("expected 2 skipped names, got %d", len(result.SkippedNames))
	}
	if len(result.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(result.Errors))
	}
}

func toJSON(v interface{}) string {
	switch v := v.(type) {
	case []map[string]interface{}:
		if len(v) == 0 {
			return "[]"
		}
		result := "["
		for i, m := range v {
			if i > 0 {
				result += ","
			}
			result += "{"
			first := true
			for k, val := range m {
				if !first {
					result += ","
				}
				result += `"` + k + `":`
				switch v := val.(type) {
				case string:
					result += `"` + v + `"`
				case bool:
					if v {
						result += "true"
					} else {
						result += "false"
					}
				default:
					result += "null"
				}
				first = false
			}
			result += "}"
		}
		result += "]"
		return result
	default:
		return "null"
	}
}
