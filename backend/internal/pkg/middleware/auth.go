package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"tenant-service/backend/internal/biz"
	pkgauth "tenant-service/backend/internal/pkg/auth"
	"tenant-service/backend/internal/pkg/casbin"
)

const (
	CtxUserID = "user_id"
	CtxDomain = "domain"
	CtxRole   = "role"
)

// Auth verifies the JWT, loads the user, rejects disabled accounts (the
// local status gate from the design spec), and stashes userID/domain/role
// in the Gin context for downstream handlers and the Authz middleware.
func Auth(issuer *pkgauth.JWTIssuer, users biz.UserRepo) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		token := strings.TrimPrefix(header, "Bearer ")
		if token == "" || token == header {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		claims, err := issuer.Verify(token)
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		u, err := users.Get(c.Request.Context(), claims.UserID)
		if err != nil || u.Status == "disabled" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(CtxUserID, u.ID)
		c.Set(CtxDomain, casbin.Domain(u.Role, u.TenantID))
		c.Set(CtxRole, string(u.Role))
		c.Next()
	}
}
