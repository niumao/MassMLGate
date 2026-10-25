package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"tenant-service/backend/internal/pkg/casbin"
)

// Authz enforces a fixed (obj, act) pair for a route, e.g.
// router.GET("/tenants", Authz(e, "tenant", "read"), handler.List).
func Authz(e *casbin.Enforcer, obj, act string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString(CtxUserID)
		domain := c.GetString(CtxDomain)
		ok, err := e.Enforce(userID, domain, obj, act)
		if err != nil || !ok {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}
}
