package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"tenant-service/backend/internal/biz"
	"tenant-service/backend/internal/pkg/middleware"
)

type WorkspaceHandler struct {
	workspaces *biz.WorkspaceBiz
	tenants    *biz.TenantBiz
}

func NewWorkspaceHandler(workspaces *biz.WorkspaceBiz, tenants *biz.TenantBiz) *WorkspaceHandler {
	return &WorkspaceHandler{workspaces: workspaces, tenants: tenants}
}

type workspaceResponse struct {
	ID           string `json:"id"`
	TenantID     string `json:"tenantId"`
	TeamID       string `json:"teamId"`
	OwnerUserID  string `json:"ownerUserId"`
	Name         string `json:"name"`
	ImageKey     string `json:"imageKey"`
	Size         string `json:"size"`
	Status       string `json:"status"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	// ConnectHint is populated once the workspace is Running — a
	// kubectl port-forward command the operator runs to actually reach the
	// workspace. Tenant Service doesn't register an Ingress/AIGatewayRoute
	// for interactive workspaces (out of scope, same as inference traffic
	// auth — see the design spec); reaching the Service is left to the
	// operator's own ingress or port-forward.
	ConnectHint string `json:"connectHint,omitempty"`
}

// toWorkspaceResponse resolves the tenant's K8s namespace to build the
// connect hint. Looked up per-call rather than carried on Workspace itself
// since the namespace is tenant data, not workspace data.
func (h *WorkspaceHandler) toWorkspaceResponse(ctx context.Context, w *biz.Workspace) workspaceResponse {
	resp := workspaceResponse{
		ID: w.ID, TenantID: w.TenantID, TeamID: w.TeamID, OwnerUserID: w.OwnerUserID,
		Name: w.Name, ImageKey: w.ImageKey, Size: w.Size, Status: string(w.Status), ErrorMessage: w.ErrorMessage,
	}
	if w.Status == biz.WorkspaceRunning && w.K8sServiceName != "" {
		if t, err := h.tenants.Get(ctx, w.TenantID); err == nil {
			port := biz.ImagePorts[w.ImageKey]
			resp.ConnectHint = fmt.Sprintf(
				"kubectl port-forward -n mlops-t-%s svc/%s %d:%d",
				t.Namespace, w.K8sServiceName, port, port,
			)
		}
	}
	return resp
}

type createWorkspaceRequest struct {
	TeamID   string `json:"teamId" binding:"required"`
	Name     string `json:"name" binding:"required"`
	ImageKey string `json:"imageKey" binding:"required"`
	Size     string `json:"size" binding:"required"`
}

func (h *WorkspaceHandler) Create(c *gin.Context) {
	var req createWorkspaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	w := &biz.Workspace{
		TenantID: callerTenantID(c), TeamID: req.TeamID, OwnerUserID: c.GetString(middleware.CtxUserID),
		Name: req.Name, ImageKey: req.ImageKey, Size: req.Size,
	}
	if err := h.workspaces.Create(c.Request.Context(), w); err != nil {
		writeWorkspaceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, h.toWorkspaceResponse(c.Request.Context(), w))
}

// List is shared: member sees their own workspaces, tenant_admin sees every
// workspace in their tenant (read-only — see WorkspaceReadAccess).
func (h *WorkspaceHandler) List(c *gin.Context) {
	if c.GetString(middleware.CtxRole) == string(biz.RoleMember) {
		list, err := h.workspaces.ListByOwner(c.Request.Context(), c.GetString(middleware.CtxUserID))
		h.writeList(c, list, err)
		return
	}
	list, err := h.workspaces.ListByTenant(c.Request.Context(), callerTenantID(c))
	h.writeList(c, list, err)
}

// ListForTenant backs the System Admin read-only drill-down
// (GET /api/v1/tenants/:id/workspaces) — tenant ID comes from the URL.
func (h *WorkspaceHandler) ListForTenant(c *gin.Context) {
	list, err := h.workspaces.ListByTenant(c.Request.Context(), c.Param("id"))
	h.writeList(c, list, err)
}

func (h *WorkspaceHandler) writeList(c *gin.Context, list []*biz.Workspace, err error) {
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	resp := make([]workspaceResponse, 0, len(list))
	for _, w := range list {
		resp = append(resp, h.toWorkspaceResponse(c.Request.Context(), w))
	}
	c.JSON(http.StatusOK, resp)
}

func (h *WorkspaceHandler) Get(c *gin.Context) {
	w, err := h.workspaces.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "workspace not found"})
		return
	}
	c.JSON(http.StatusOK, h.toWorkspaceResponse(c.Request.Context(), w))
}

func (h *WorkspaceHandler) Start(c *gin.Context) {
	err := h.workspaces.Start(c.Request.Context(), c.Param("id"), c.GetString(middleware.CtxUserID))
	writeWorkspaceActionResult(c, err)
}

func (h *WorkspaceHandler) Stop(c *gin.Context) {
	err := h.workspaces.Stop(c.Request.Context(), c.Param("id"), c.GetString(middleware.CtxUserID))
	writeWorkspaceActionResult(c, err)
}

func (h *WorkspaceHandler) Delete(c *gin.Context) {
	err := h.workspaces.Delete(c.Request.Context(), c.Param("id"), c.GetString(middleware.CtxUserID))
	if err != nil {
		writeWorkspaceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func writeWorkspaceActionResult(c *gin.Context, err error) {
	if err != nil {
		writeWorkspaceError(c, err)
		return
	}
	c.Status(http.StatusOK)
}

func writeWorkspaceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, biz.ErrUnknownImage), errors.Is(err, biz.ErrSizePresetNotFound), errors.Is(err, biz.ErrTeamQuotaExceeded):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, biz.ErrNotWorkspaceOwner):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

// Catalog returns the image/size options and the given team's remaining
// quota — team_id is required since a member may belong to multiple teams.
type CatalogHandler struct {
	teams       *biz.TeamBiz
	sizePresets *biz.SizePresetBiz
}

func NewCatalogHandler(teams *biz.TeamBiz, sizePresets *biz.SizePresetBiz) *CatalogHandler {
	return &CatalogHandler{teams: teams, sizePresets: sizePresets}
}

func (h *CatalogHandler) Get(c *gin.Context) {
	teamID := c.Query("team_id")
	if teamID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "team_id is required"})
		return
	}
	team, err := h.teams.GetForMember(c.Request.Context(), teamID, c.GetString(middleware.CtxUserID))
	if err != nil {
		if errors.Is(err, biz.ErrNotTeamMember) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "team not found"})
		return
	}

	images := make([]string, len(biz.ImageKeys))
	copy(images, biz.ImageKeys)

	presets, err := h.sizePresets.ListByTenant(c.Request.Context(), team.TenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	sizes := make([]gin.H, 0, len(presets))
	for _, p := range presets {
		sizes = append(sizes, gin.H{"size": p.Name, "cpu": p.CPURequest, "memory": p.MemoryRequest, "gpu": p.GPURequest})
	}

	c.JSON(http.StatusOK, gin.H{
		"images":    images,
		"sizes":     sizes,
		"teamQuota": gin.H{"cpu": team.QuotaCPU, "memory": team.QuotaMemory, "gpu": team.QuotaGPU},
	})
}
