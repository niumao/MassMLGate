package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"tenant-service/backend/internal/biz"
	"tenant-service/backend/internal/pkg/middleware"
)

type UserHandler struct {
	users  *biz.UserBiz
	hasher biz.PasswordHasher
}

func NewUserHandler(users *biz.UserBiz, hasher biz.PasswordHasher) *UserHandler {
	return &UserHandler{users: users, hasher: hasher}
}

func callerTenantID(c *gin.Context) string {
	// Domain is "tenant:{id}" for tenant-scoped callers (see pkg/casbin.Domain).
	domain := c.GetString(middleware.CtxDomain)
	return domain[len("tenant:"):]
}

type createUserRequest struct {
	Email       string `json:"email" binding:"required"`
	Password    string `json:"password" binding:"required"`
	DisplayName string `json:"displayName" binding:"required"`
	Role        string `json:"role" binding:"required"`
}

func (h *UserHandler) Create(c *gin.Context) {
	h.createForTenant(c, callerTenantID(c))
}

// CreateForTenant backs the System Admin rescue/bootstrap path
// (POST /api/v1/tenants/:id/users) — tenant ID comes from the URL, not the
// caller's own domain (System Admin's domain is "system").
func (h *UserHandler) CreateForTenant(c *gin.Context) {
	h.createForTenant(c, c.Param("id"))
}

func (h *UserHandler) createForTenant(c *gin.Context, tenantID string) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	hash, err := h.hasher.Hash(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}
	u := &biz.User{
		TenantID: &tenantID, Email: req.Email, PasswordHash: hash,
		DisplayName: req.DisplayName, Role: biz.Role(req.Role), Status: "active",
	}
	if err := h.users.Create(c.Request.Context(), u); err != nil {
		if errors.Is(err, biz.ErrCannotEscalateRole) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, toUserResponse(u))
}

func (h *UserHandler) List(c *gin.Context) {
	h.listByTenant(c, callerTenantID(c))
}

// ListForTenant backs the System Admin drill-down/rescue path
// (GET /api/v1/tenants/:id/users).
func (h *UserHandler) ListForTenant(c *gin.Context) {
	h.listByTenant(c, c.Param("id"))
}

func (h *UserHandler) listByTenant(c *gin.Context, tenantID string) {
	list, err := h.users.ListByTenant(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	resp := make([]userResponse, 0, len(list))
	for _, u := range list {
		resp = append(resp, toUserResponse(u))
	}
	c.JSON(http.StatusOK, resp)
}

type updateUserRequest struct {
	DisplayName *string `json:"displayName"`
	Role        *string `json:"role"`
	Status      *string `json:"status"` // "active" | "disabled"
}

func (h *UserHandler) Update(c *gin.Context) {
	var req updateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id := c.Param("user_id")

	if req.Status != nil {
		var err error
		if *req.Status == "disabled" {
			err = h.users.Disable(c.Request.Context(), id)
		} else {
			err = h.users.Enable(c.Request.Context(), id)
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	u, err := h.users.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if req.DisplayName != nil {
		u.DisplayName = *req.DisplayName
	}
	if req.Role != nil {
		u.Role = biz.Role(*req.Role)
	}
	if req.DisplayName != nil || req.Role != nil {
		if err := h.users.Update(c.Request.Context(), u); err != nil {
			if errors.Is(err, biz.ErrCannotEscalateRole) {
				c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, toUserResponse(u))
}

func (h *UserHandler) Delete(c *gin.Context) {
	if err := h.users.Delete(c.Request.Context(), c.Param("user_id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
