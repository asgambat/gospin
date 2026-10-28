package controller

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/asgambat/gospin/internal/config"
	"github.com/asgambat/gospin/internal/logger"
)

// DefaultActionsConfigPath is the default path for the actions.yml configuration file.
const DefaultActionsConfigPath = "./config/actions.yml"

// ActionController handles action execution endpoints. It acts as a server-side
// HTTP proxy: the browser sends an action name, and this controller looks up
// the technical details (method, URL, headers, body) from actions.yml and
// executes the request server-side.
type ActionController struct {
	actionsCache   *config.ActionsFile
	actionsCfgPath string
	actionsMode    string
	timeout        time.Duration

	// mu guards actionsCache and loadFailureLogged. Requests may recover the
	// configuration at runtime (see actionsConfig), so the cached pointer is
	// replaced while other goroutines read it. Declared after the fields above
	// because govet's fieldalignment wants the pointer-free data last.
	mu                sync.RWMutex
	loadFailureLogged bool
}

// NewActionController creates a new ActionController.
func NewActionController(actionsCfgPath, actionsMode string, timeout time.Duration) *ActionController {
	if actionsMode == "" {
		actionsMode = "async"
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	ac := &ActionController{
		actionsCfgPath: actionsCfgPath,
		actionsMode:    actionsMode,
		timeout:        timeout,
	}

	// Load the configuration once at startup and cache it. A missing file is
	// not an error: LoadActionsConfig returns an empty action set, so any error
	// reported here means the file exists but cannot be used (invalid YAML,
	// missing method/url, unsupported URL scheme).
	if actionsCfg, err := config.LoadActionsConfig(actionsCfgPath); err != nil {
		logger.WithComponent("actions_controller").Warnf("failed to load actions config from %s: %v", actionsCfgPath, err)
		ac.loadFailureLogged = true
	} else {
		ac.actionsCache = actionsCfg
		if len(actionsCfg.Actions) == 0 {
			logger.WithComponent("actions_controller").Infof("no actions defined in %s (create it from actions.yml.example to enable actions)", actionsCfgPath)
		}
	}

	return ac
}

// actionsConfig returns the action definitions, loading them on demand when the
// startup load failed (for example because actions.yml had a syntax error and
// was fixed afterwards). The mutex keeps concurrent first requests from writing
// the cached pointer at the same time and from re-parsing the file in a loop.
func (ac *ActionController) actionsConfig() (*config.ActionsFile, error) {
	ac.mu.RLock()
	cached := ac.actionsCache
	ac.mu.RUnlock()
	if cached != nil {
		return cached, nil
	}

	ac.mu.Lock()
	defer ac.mu.Unlock()
	if ac.actionsCache != nil { // loaded by a concurrent request while we waited
		return ac.actionsCache, nil
	}

	cfg, err := config.LoadActionsConfig(ac.actionsCfgPath)
	if err != nil {
		// Log the first failure only: while the file stays broken every request
		// would otherwise repeat the same error line.
		if !ac.loadFailureLogged {
			logger.WithComponent("actions_controller").Errorf("failed to load actions config from %s: %v", ac.actionsCfgPath, err)
			ac.loadFailureLogged = true
		}
		return nil, err
	}
	ac.actionsCache = cfg
	ac.loadFailureLogged = false
	return cfg, nil
}

// cachedActions returns the currently loaded definitions without triggering a
// load, for callers that must not parse the file (startup checks).
func (ac *ActionController) cachedActions() *config.ActionsFile {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.actionsCache
}

// Timeout returns the configured per-action HTTP timeout.
func (ac *ActionController) Timeout() time.Duration {
	return ac.timeout
}

// actionRequest is the JSON body sent by the frontend.
type actionRequest struct {
	ActionName string `json:"actionName" binding:"required"`
}

// LogDanglingReferences warns about action names referenced in homepage.yaml
// (via the visual actions section) that are not defined in actions.yml. This is
// a non-fatal startup check: a dangling reference would otherwise only fail at
// click time with a generic 404. It returns the list of dangling names found.
func (ac *ActionController) LogDanglingReferences(referenced []string) []string {
	var dangling []string
	if len(referenced) == 0 {
		return dangling
	}

	actionsCfg := ac.cachedActions()
	if actionsCfg == nil {
		logger.WithComponent("actions_controller").Warnf("skipping dangling-reference check, actions config not loaded")
		return dangling
	}
	if len(actionsCfg.Actions) == 0 {
		// actions.yml is optional: with no defined action every reference would
		// look dangling, so a fresh checkout would log one warning per homepage
		// entry. Nothing can be resolved, so the check is skipped.
		logger.WithComponent("actions_controller").Debugf("skipping dangling-reference check, no actions defined in %s", ac.actionsCfgPath)
		return dangling
	}

	for _, name := range referenced {
		if name == "" {
			// A homepage item without an action reference has nothing to resolve.
			continue
		}
		if _, ok := actionsCfg.Actions[name]; !ok {
			dangling = append(dangling, name)
			logger.WithComponent("actions_controller").Warnf("action %q is referenced in homepage.yaml but not defined in %s", name, ac.actionsCfgPath)
		}
	}
	return dangling
}

// actionResponse is the JSON response returned to the frontend.
// In sync mode, StatusCode and Body are both populated.
// In async mode, only StatusCode is populated.
// Mode is always returned so the frontend can render the correct layout
// instead of inferring it from the presence of a body.
type actionResponse struct {
	Body       string `json:"body,omitempty"`
	Mode       string `json:"mode"`
	StatusCode int    `json:"statusCode"`
}

// errorResponse writes a JSON error body that also carries the configured
// action mode, so the frontend can render the result modal consistently in
// every branch (success and error) instead of inferring mode from the body.
func (ac *ActionController) errorResponse(c *gin.Context, status int, format string, args ...interface{}) {
	c.JSON(status, gin.H{
		"error": fmt.Sprintf(format, args...),
		"mode":  ac.actionsMode,
	})
}

// ExecuteAction handles POST /homepage/action. It looks up the action by name,
// executes the configured HTTP request server-side, and returns the result.
func (ac *ActionController) ExecuteAction(c *gin.Context) {
	var req actionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		ac.errorResponse(c, http.StatusBadRequest, "actionName is required")
		return
	}

	// Use the cached configuration; it is loaded on demand (and retried) only
	// when the startup load failed.
	actionsCfg, err := ac.actionsConfig()
	if err != nil {
		// Already logged once by actionsConfig; do not repeat the same failure.
		ac.errorResponse(c, http.StatusInternalServerError, "actions configuration unavailable")
		return
	}

	actionCfg, ok := actionsCfg.Actions[req.ActionName]
	if !ok {
		logger.WithComponent("actions_controller").Warnf("action %q not found in actions config", req.ActionName)
		ac.errorResponse(c, http.StatusNotFound, "action %q not found", req.ActionName)
		return
	}

	// Build the HTTP request
	httpReq, err := http.NewRequest(actionCfg.Method, actionCfg.URL, bytes.NewBufferString(actionCfg.Body))
	if err != nil {
		logger.WithComponent("actions_controller").Errorf("failed to build HTTP request for action %q: %v", req.ActionName, err)
		ac.errorResponse(c, http.StatusInternalServerError, "failed to build HTTP request")
		return
	}

	// Honor the request context so client disconnects and the gin request
	// timeout (configured per-route from actions_timeout_seconds) actually
	// abort the outbound call instead of letting it run until its own timer.
	httpReq = httpReq.WithContext(c.Request.Context())

	for key, value := range actionCfg.Headers {
		httpReq.Header.Set(key, value)
	}

	// Execute the request
	client := &http.Client{Timeout: ac.timeout}
	logger.WithComponent("actions_controller").Infof("executing action %q: %s %s (mode=%s)", req.ActionName, actionCfg.Method, actionCfg.URL, ac.actionsMode)

	resp, err := client.Do(httpReq)
	if err != nil {
		logger.WithComponent("actions_controller").Errorf("action %q HTTP request failed: %v", req.ActionName, err)
		ac.errorResponse(c, http.StatusBadGateway, "action %q failed: %v", req.ActionName, err)
		return
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			logger.WithComponent("actions_controller").Debugf("failed to close response body for action %q: %v", req.ActionName, cerr)
		}
	}()

	// Build the response
	result := actionResponse{
		StatusCode: resp.StatusCode,
		Mode:       ac.actionsMode,
	}

	// In sync mode, also return the response body
	if ac.actionsMode == "sync" {
		bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB limit
		if err != nil {
			logger.WithComponent("actions_controller").Warnf("action %q: failed to read response body: %v", req.ActionName, err)
		} else {
			result.Body = string(bodyBytes)
		}
	}

	logger.WithComponent("actions_controller").Infof("action %q completed: status=%d (mode=%s)", req.ActionName, resp.StatusCode, ac.actionsMode)
	c.JSON(http.StatusOK, result)
}
