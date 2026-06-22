package middleware

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	honeybadger "github.com/honeybadger-io/honeybadger-go"
	"github.com/sirupsen/logrus"
)

// HoneybadgerMiddleware sends error/warning notifications to Honeybadger.
// On panic, it notifies Honeybadger and re-panics to allow gin.Recovery to handle the response.
func HoneybadgerMiddleware(logger *logrus.Logger) gin.HandlerFunc {
	apiKey := os.Getenv("HONEYBADGER_API_KEY")
	if apiKey == "" {
		logger.Info("Honeybadger is not active. To enable error reporting, set the HONEYBADGER_API_KEY environment variable.")
		return func(c *gin.Context) {
			c.Next()
		}
	}

	honeybadger.Configure(honeybadger.Configuration{
		APIKey: apiKey,
		Env:    os.Getenv("GO_ENV"),
	})

	logger.Info("Honeybadger error reporting is enabled.")

	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				// Notify Honeybadger with stacktrace, then re-panic.
				// Honeybadger delivery failures are logged but do not block panic re-raise.
				// Notify returns (noticeID string, error); we only care about the error here.
				if _, err := honeybadger.Notify(fmt.Sprintf("Panic: %s %s", c.Request.Method, c.Request.URL.Path),
					c.Request, honeybadger.Context{"stack": string(debug.Stack())}, honeybadger.Tags{"panic", "http"}); err != nil {
					logger.Debugf("Honeybadger notify failed (non-critical): %v", err)
				}
				logger.Error("Recovered from panic, notified Honeybadger: ", rec)
				panic(rec) // propagate panic to let gin.Recovery handle it
			}
		}()

		c.Next()

		status := c.Writer.Status()
		if status >= 400 && status != 404 {
			if status >= 500 {
				// Send stacktrace for 5xx errors
				if _, err := honeybadger.Notify(fmt.Sprintf("Error: HTTP %d: %s %s", status, c.Request.Method, c.Request.URL.Path), c.Request, honeybadger.Tags{"5XX", "http"}); err != nil {
					logger.Debugf("Honeybadger notify failed (non-critical): %v", err)
				}
			} else {
				// For warnings (4xx), send as notice without stacktrace
				if _, err := honeybadger.Notify(fmt.Sprintf("Warning: HTTP %d: %s %s", status, c.Request.Method, c.Request.URL.Path), honeybadger.Tags{"4XX", "http"}); err != nil {
					logger.Debugf("Honeybadger notify failed (non-critical): %v", err)
				}
			}
			logger.Warnf("Honeybadger reported HTTP %d for %s %s", status, c.Request.Method, c.Request.URL.Path)
		}
	}
}
