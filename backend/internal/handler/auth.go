package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"tenant-service/backend/internal/biz"
	pkgauth "tenant-service/backend/internal/pkg/auth"
	"tenant-service/backend/internal/pkg/casbin"
	"tenant-service/backend/internal/pkg/middleware"
)

type AuthHandler struct {
	auth     *biz.AuthBiz
	users    biz.UserRepo
	issuer   *pkgauth.JWTIssuer
	enforcer *casbin.Enforcer
}

func NewAuthHandler(auth *biz.AuthBiz, users biz.UserRepo, issuer *pkgauth.JWTIssuer, enforcer *casbin.Enforcer) *AuthHandler {
	return &AuthHandler{auth: auth, users: users, issuer: issuer, enforcer: enforcer}
}

type loginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type userResponse struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	DisplayName string  `json:"displayName"`
	Role        string  `json:"role"`
	TenantID    *string `json:"tenantId"`
	Status      string  `json:"status"`
}

func toUserResponse(u *biz.User) userResponse {
	return userResponse{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, Role: string(u.Role), TenantID: u.TenantID, Status: u.Status}
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	u, err := h.auth.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	var tenantID string
	if u.TenantID != nil {
		tenantID = *u.TenantID
	}
	token, err := h.issuer.Sign(u.ID, tenantID, string(u.Role))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sign token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "user": toUserResponse(u)})
}

// Logout is a no-op beyond instructing the client to discard its token —
// JWTs are stateless in this spec, there's no server-side session store.
func (h *AuthHandler) Logout(c *gin.Context) {
	c.Status(http.StatusOK)
}

func (h *AuthHandler) Me(c *gin.Context) {
	userID := c.GetString(middleware.CtxUserID)
	domain := c.GetString(middleware.CtxDomain)
	u, err := h.users.Get(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user": toUserResponse(u),
		"menu": h.enforcer.FilterMenu(userID, domain),
	})
}
