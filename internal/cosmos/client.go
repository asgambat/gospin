package cosmos

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/bassista/go_spin/internal/logger"
)

type RouteResponse struct {
	Status string      `json:"status"`
	Data   []RouteItem `json:"data"`
}

type RouteItem struct {
	Mode          string `json:"Mode"`
	Name          string `json:"Name"`
	Target        string `json:"Target"`
	Description   string `json:"Description"`
	Host          string `json:"Host"`
	PathPrefix    string `json:"PathPrefix"`
	Disabled      bool   `json:"Disabled"`
	UseHost       bool   `json:"UseHost"`
	UsePathPrefix bool   `json:"UsePathPrefix"`
}

type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) FetchRoutes(ctx context.Context, baseUrl, token string) ([]RouteItem, error) {
	url := fmt.Sprintf("%s/cosmos/api/routes", baseUrl)
	logger.WithComponent("cosmos-client").Debugf("fetching routes from: %s", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("accept", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch routes: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			logger.WithComponent("cosmos-client").Debugf("failed to close response body: %v", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("API returned status %d (and body read failed: %w)", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var routeResp RouteResponse
	if err := json.NewDecoder(resp.Body).Decode(&routeResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if routeResp.Status != "OK" {
		return nil, fmt.Errorf("API returned status: %s", routeResp.Status)
	}

	logger.WithComponent("cosmos-client").Debugf("fetched %d routes from cosmos", len(routeResp.Data))
	return routeResp.Data, nil
}

func FilterValidRoutes(routes []RouteItem) []RouteItem {
	var valid []RouteItem
	for _, route := range routes {
		if !route.Disabled && route.Mode == "SERVAPP" && route.UseHost {
			valid = append(valid, route)
		}
	}
	logger.WithComponent("cosmos-client").Debugf("filtered %d valid routes from %d total", len(valid), len(routes))
	return valid
}
