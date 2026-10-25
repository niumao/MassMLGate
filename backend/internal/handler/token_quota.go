package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"tenant-service/backend/internal/biz"
)

type TokenQuotaHandler struct {
	quotas *biz.TokenQuotaBiz
}

func NewTokenQuotaHandler(quotas *biz.TokenQuotaBiz) *TokenQuotaHandler {
	return &TokenQuotaHandler{quotas: quotas}
}

type tokenQuotaResponse struct {
	ID         string `json:"id"`
	TenantID   string `json:"tenantId"`
	ModelName  string `json:"modelName"`
	TokenLimit int64  `json:"tokenLimit"`
	Window     string `json:"window"`
}

func toTokenQuotaResponse(q *biz.TokenQuota) tokenQuotaResponse {
	return tokenQuotaResponse{
		ID: q.ID, TenantID: q.TenantID, ModelName: q.ModelName, TokenLimit: q.TokenLimit, Window: q.Window,
	}
}

type createTokenQuotaRequest struct {
	ModelName  string `json:"modelName" binding:"required"`
	TokenLimit int64  `json:"tokenLimit" binding:"gte=0"`
	Window     string `json:"window" binding:"required,oneof=daily monthly"`
}

func (h *TokenQuotaHandler) Create(c *gin.Context) {
	var req createTokenQuotaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	q := &biz.TokenQuota{
		TenantID: callerTenantID(c), ModelName: req.ModelName, TokenLimit: req.TokenLimit, Window: req.Window,
	}
	if err := h.quotas.Create(c.Request.Context(), q); err != nil {
		writeTokenQuotaError(c, err)
		return
	}
	c.JSON(http.StatusCreated, toTokenQuotaResponse(q))
}

func (h *TokenQuotaHandler) List(c *gin.Context) {
	h.listByTenant(c, callerTenantID(c))
}

// ListForTenant backs the System Admin read-only drill-down
// (GET /api/v1/tenants/:id/token-quotas) — tenant ID comes from the URL,
// not the caller's own domain. Same pattern as TeamHandler.ListForTenant.
func (h *TokenQuotaHandler) ListForTenant(c *gin.Context) {
	h.listByTenant(c, c.Param("id"))
}

func (h *TokenQuotaHandler) listByTenant(c *gin.Context, tenantID string) {
	list, err := h.quotas.ListByTenant(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	resp := make([]tokenQuotaResponse, 0, len(list))
	for _, q := range list {
		resp = append(resp, toTokenQuotaResponse(q))
	}
	c.JSON(http.StatusOK, resp)
}

type updateTokenQuotaRequest struct {
	ModelName  string `json:"modelName" binding:"required"`
	TokenLimit int64  `json:"tokenLimit" binding:"gte=0"`
	Window     string `json:"window" binding:"required,oneof=daily monthly"`
}

func (h *TokenQuotaHandler) Update(c *gin.Context) {
	var req updateTokenQuotaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	q, err := h.quotas.Update(c.Request.Context(), callerTenantID(c), c.Param("id"), req.ModelName, req.TokenLimit, req.Window)
	if err != nil {
		writeTokenQuotaError(c, err)
		return
	}
	c.JSON(http.StatusOK, toTokenQuotaResponse(q))
}

func (h *TokenQuotaHandler) Delete(c *gin.Context) {
	if err := h.quotas.Delete(c.Request.Context(), callerTenantID(c), c.Param("id")); err != nil {
		writeTokenQuotaError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func writeTokenQuotaError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, biz.ErrTokenQuotaNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
