package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"tenant-service/backend/internal/biz"
)

type TenantHandler struct {
	tenants *biz.TenantBiz
}

func NewTenantHandler(tenants *biz.TenantBiz) *TenantHandler {
	return &TenantHandler{tenants: tenants}
}

type tenantResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	QuotaCPU    int    `json:"quotaCpu"`
	QuotaMemory int64  `json:"quotaMemory"`
	QuotaGPU    int    `json:"quotaGpu"`
	Status      string `json:"status"`
}

func toTenantResponse(t *biz.Tenant) tenantResponse {
	return tenantResponse{
		ID: t.ID, Name: t.Name, Namespace: t.Namespace,
		QuotaCPU: t.QuotaCPU, QuotaMemory: t.QuotaMemory, QuotaGPU: t.QuotaGPU,
		Status: t.Status,
	}
}

type createTenantRequest struct {
	Name        string `json:"name" binding:"required"`
	Namespace   string `json:"namespace" binding:"required"`
	QuotaCPU    int    `json:"quotaCpu"`
	QuotaMemory int64  `json:"quotaMemory"`
	QuotaGPU    int    `json:"quotaGpu"`
}

func (h *TenantHandler) Create(c *gin.Context) {
	var req createTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t := &biz.Tenant{Name: req.Name, Namespace: req.Namespace, QuotaCPU: req.QuotaCPU, QuotaMemory: req.QuotaMemory, QuotaGPU: req.QuotaGPU}
	if err := h.tenants.Create(c.Request.Context(), t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, toTenantResponse(t))
}

func (h *TenantHandler) List(c *gin.Context) {
	list, err := h.tenants.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	resp := make([]tenantResponse, 0, len(list))
	for _, t := range list {
		resp = append(resp, toTenantResponse(t))
	}
	c.JSON(http.StatusOK, resp)
}

func (h *TenantHandler) Get(c *gin.Context) {
	t, err := h.tenants.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "tenant not found"})
		return
	}
	c.JSON(http.StatusOK, toTenantResponse(t))
}

type updateTenantQuotaRequest struct {
	QuotaCPU    int   `json:"quotaCpu"`
	QuotaMemory int64 `json:"quotaMemory"`
	QuotaGPU    int   `json:"quotaGpu"`
}

func (h *TenantHandler) Update(c *gin.Context) {
	var req updateTenantQuotaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t, err := h.tenants.UpdateQuota(c.Request.Context(), c.Param("id"), req.QuotaCPU, req.QuotaMemory, req.QuotaGPU)
	if err != nil {
		if errors.Is(err, biz.ErrQuotaBelowTeamCommitments) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toTenantResponse(t))
}

func (h *TenantHandler) Delete(c *gin.Context) {
	if err := h.tenants.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
