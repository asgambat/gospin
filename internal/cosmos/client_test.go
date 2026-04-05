package cosmos

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClient(t *testing.T) {
	client := NewClient()
	if client == nil {
		t.Fatal("expected client to be created")
	}
	if client.httpClient == nil {
		t.Fatal("expected httpClient to be initialized")
	}
}

func TestFetchRoutes(t *testing.T) {
	tests := []struct {
		name           string
		responseStatus int
		responseBody   string
		expectError    bool
		expectedCount  int
	}{
		{
			name:           "successful fetch with valid routes",
			responseStatus: http.StatusOK,
			responseBody: `{
				"data": [
					{"Disabled": false, "Mode": "PROXY", "Name": "test1", "Description": "test desc 1", "UseHost": true, "Host": "host1.example.com", "UsePathPrefix": false, "PathPrefix": ""},
					{"Disabled": false, "Mode": "PROXY", "Name": "test2", "Description": "test desc 2", "UseHost": true, "Host": "host2.example.com", "UsePathPrefix": false, "PathPrefix": ""}
				],
				"status": "OK"
			}`,
			expectError:   false,
			expectedCount: 2,
		},
		{
			name:           "empty response",
			responseStatus: http.StatusOK,
			responseBody: `{
				"data": [],
				"status": "OK"
			}`,
			expectError:   false,
			expectedCount: 0,
		},
		{
			name:           "server returns error status",
			responseStatus: http.StatusInternalServerError,
			responseBody:   "internal server error",
			expectError:    true,
			expectedCount:  0,
		},
		{
			name:           "non-OK cosmos status",
			responseStatus: http.StatusOK,
			responseBody: `{
				"data": [],
				"status": "ERROR"
			}`,
			expectError:   true,
			expectedCount: 0,
		},
		{
			name:           "invalid json response",
			responseStatus: http.StatusOK,
			responseBody:   "not valid json",
			expectError:    true,
			expectedCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request headers
				if r.Header.Get("accept") != "application/json" {
					t.Errorf("expected accept header to be application/json, got %s", r.Header.Get("accept"))
				}
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Errorf("expected Authorization header to be Bearer test-token, got %s", r.Header.Get("Authorization"))
				}

				w.WriteHeader(tt.responseStatus)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			client := NewClient()
			routes, err := client.FetchRoutes(context.Background(), server.URL, "test-token")

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if len(routes) != tt.expectedCount {
					t.Errorf("expected %d routes, got %d", tt.expectedCount, len(routes))
				}
			}
		})
	}
}

func TestFilterValidRoutes(t *testing.T) {
	tests := []struct {
		name          string
		input         []RouteItem
		expectedCount int
		expectedNames []string
	}{
		{
			name: "all valid routes",
			input: []RouteItem{
				{Disabled: false, Mode: "SERVAPP", Name: "valid1", Target: "http://container1:8080", UseHost: true, Host: "host1.com"},
				{Disabled: false, Mode: "SERVAPP", Name: "valid2", Target: "http://container2:8080", UseHost: true, Host: "host2.com"},
			},
			expectedCount: 2,
			expectedNames: []string{"valid1", "valid2"},
		},
		{
			name: "disabled route should be filtered",
			input: []RouteItem{
				{Disabled: false, Mode: "SERVAPP", Name: "valid", Target: "http://valid:8080", UseHost: true, Host: "host.com"},
				{Disabled: true, Mode: "SERVAPP", Name: "disabled", Target: "http://disabled:8080", UseHost: true, Host: "disabled.com"},
			},
			expectedCount: 1,
			expectedNames: []string{"valid"},
		},
		{
			name: "non-SERVAPP mode should be filtered",
			input: []RouteItem{
				{Disabled: false, Mode: "SERVAPP", Name: "valid", Target: "http://valid:8080", UseHost: true, Host: "host.com"},
				{Disabled: false, Mode: "STATIC", Name: "static", Target: "http://static:8080", UseHost: true, Host: "static.com"},
				{Disabled: false, Mode: "REDIRECT", Name: "redirect", Target: "http://redirect:8080", UseHost: true, Host: "redirect.com"},
			},
			expectedCount: 1,
			expectedNames: []string{"valid"},
		},
		{
			name: "non-UseHost route should be filtered",
			input: []RouteItem{
				{Disabled: false, Mode: "SERVAPP", Name: "valid", Target: "http://valid:8080", UseHost: true, Host: "host.com"},
				{Disabled: false, Mode: "SERVAPP", Name: "noHost", Target: "http://nohost:8080", UseHost: false, Host: "nohost.com"},
			},
			expectedCount: 1,
			expectedNames: []string{"valid"},
		},
		{
			name: "multiple filters should all apply",
			input: []RouteItem{
				{Disabled: true, Mode: "SERVAPP", Name: "disabled", Target: "http://disabled:8080", UseHost: true, Host: "host.com"},
				{Disabled: false, Mode: "STATIC", Name: "static", Target: "http://static:8080", UseHost: true, Host: "static.com"},
				{Disabled: false, Mode: "SERVAPP", Name: "noHost", Target: "http://nohost:8080", UseHost: false, Host: "nohost.com"},
				{Disabled: false, Mode: "SERVAPP", Name: "valid", Target: "http://valid:8080", UseHost: true, Host: "host.com"},
			},
			expectedCount: 1,
			expectedNames: []string{"valid"},
		},
		{
			name:          "empty input",
			input:         []RouteItem{},
			expectedCount: 0,
			expectedNames: []string{},
		},
		{
			name:          "nil input",
			input:         nil,
			expectedCount: 0,
			expectedNames: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterValidRoutes(tt.input)
			if len(result) != tt.expectedCount {
				t.Errorf("expected %d routes, got %d", tt.expectedCount, len(result))
			}

			for i, expectedName := range tt.expectedNames {
				if i < len(result) && result[i].Name != expectedName {
					t.Errorf("expected route at index %d to be %s, got %s", i, expectedName, result[i].Name)
				}
			}
		})
	}
}

func TestFilterValidRoutesCaseSensitivity(t *testing.T) {
	tests := []struct {
		name          string
		mode          string
		shouldInclude bool
	}{
		{"exact SERVAPP", "SERVAPP", true},
		{"lowercase servapp", "servapp", false},
		{"mixed case ServApp", "ServApp", false},
		{"uppercase SERVAPP with spaces", " SERVAPP", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := []RouteItem{
				{Disabled: false, Mode: tt.mode, Name: "test", Target: "http://test:8080", UseHost: true, Host: "host.com"},
			}
			result := FilterValidRoutes(input)

			if tt.shouldInclude && len(result) == 0 {
				t.Errorf("expected route to be included with mode '%s'", tt.mode)
			}
			if !tt.shouldInclude && len(result) > 0 {
				t.Errorf("expected route to be filtered with mode '%s'", tt.mode)
			}
		})
	}
}
