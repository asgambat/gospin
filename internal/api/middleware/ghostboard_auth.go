package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/asgambat/gospin/internal/ghostboard"
)

// GhostboardPIN gates the boards listed in ghostboard.private_boards. A request
// for a private board needs a valid session, carried by the HttpOnly cookie the
// unlock endpoint sets or by an Authorization bearer token, which is what
// gb-cli and shell scripts use (EventSource cannot set a request header, so the
// browser relies on the cookie).
//
// A nil authenticator means no PIN is configured: every board is public and the
// middleware is a no-op.
func GhostboardPIN(auth *ghostboard.Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		if auth == nil {
			c.Next()
			return
		}

		board := strings.TrimSpace(c.Param("board"))
		if board == "" {
			board = ghostboard.DefaultBoard
		}
		if !auth.IsPrivate(board) || auth.ValidToken(ghostboard.SessionToken(c.Request)) {
			c.Next()
			return
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error": ghostboard.ErrPINRequired.Error(),
			"board": board,
		})
	}
}
