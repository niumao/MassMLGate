package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"tenant-service/backend/internal/biz"
	"tenant-service/backend/internal/pkg/middleware"
)

type TeamHandler struct {
	teams *biz.TeamBiz
}

func NewTeamHandler(teams *biz.TeamBiz) *TeamHandler {
	return &TeamHandler{teams: teams}
}

type teamResponse struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenantId"`
	Name        string `json:"name"`
	QuotaCPU    int    `json:"quotaCpu"`
	QuotaMemory int64  `json:"quotaMemory"`
	QuotaGPU    int    `json:"quotaGpu"`
}

func toTeamResponse(t *biz.Team) teamResponse {
	return teamResponse{ID: t.ID, TenantID: t.TenantID, Name: t.Name, QuotaCPU: t.QuotaCPU, QuotaMemory: t.QuotaMemory, QuotaGPU: t.QuotaGPU}
}

type createTeamRequest struct {
	Name        string `json:"name" binding:"required"`
	QuotaCPU    int    `json:"quotaCpu"`
	QuotaMemory int64  `json:"quotaMemory"`
	QuotaGPU    int    `json:"quotaGpu"`
}

func (h *TeamHandler) Create(c *gin.Context) {
	var req createTeamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t := &biz.Team{TenantID: callerTenantID(c), Name: req.Name, QuotaCPU: req.QuotaCPU, QuotaMemory: req.QuotaMemory, QuotaGPU: req.QuotaGPU}
	if err := h.teams.Create(c.Request.Context(), t); err != nil {
		writeTeamQuotaError(c, err)
		return
	}
	c.JSON(http.StatusCreated, toTeamResponse(t))
}

// List: tenant_admin sees every team in their tenant; member sees only the
// team(s) they belong to (Casbin's TeamReadAccess only scopes by tenant
// domain, not membership, so this is enforced here).
func (h *TeamHandler) List(c *gin.Context) {
	if c.GetString(middleware.CtxRole) == string(biz.RoleMember) {
		list, err := h.teams.ListByMember(c.Request.Context(), c.GetString(middleware.CtxUserID))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		resp := make([]teamResponse, 0, len(list))
		for _, t := range list {
			resp = append(resp, toTeamResponse(t))
		}
		c.JSON(http.StatusOK, resp)
		return
	}
	h.listByTenant(c, callerTenantID(c))
}

// ListForTenant backs the System Admin read-only drill-down
// (GET /api/v1/tenants/:id/teams) — tenant ID comes from the URL, not the
// caller's own domain.
func (h *TeamHandler) ListForTenant(c *gin.Context) {
	h.listByTenant(c, c.Param("id"))
}

func (h *TeamHandler) listByTenant(c *gin.Context, tenantID string) {
	list, err := h.teams.ListByTenant(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	resp := make([]teamResponse, 0, len(list))
	for _, t := range list {
		resp = append(resp, toTeamResponse(t))
	}
	c.JSON(http.StatusOK, resp)
}

// Get is shared by tenant_admin (any team in their tenant, via TeamEditAccess)
// and member (own team(s) only, via TeamReadAccess — enforced here since
// Casbin only scopes by tenant domain, not team membership).
func (h *TeamHandler) Get(c *gin.Context) {
	teamID := c.Param("team_id")
	var t *biz.Team
	var err error
	if c.GetString(middleware.CtxRole) == string(biz.RoleMember) {
		t, err = h.teams.GetForMember(c.Request.Context(), teamID, c.GetString(middleware.CtxUserID))
		if errors.Is(err, biz.ErrNotTeamMember) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
	} else {
		t, err = h.teams.Get(c.Request.Context(), teamID)
	}
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "team not found"})
		return
	}
	c.JSON(http.StatusOK, toTeamResponse(t))
}

type updateTeamQuotaRequest struct {
	QuotaCPU    int   `json:"quotaCpu"`
	QuotaMemory int64 `json:"quotaMemory"`
	QuotaGPU    int   `json:"quotaGpu"`
}

func (h *TeamHandler) Update(c *gin.Context) {
	var req updateTeamQuotaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t, err := h.teams.UpdateQuota(c.Request.Context(), c.Param("team_id"), req.QuotaCPU, req.QuotaMemory, req.QuotaGPU)
	if err != nil {
		writeTeamQuotaError(c, err)
		return
	}
	c.JSON(http.StatusOK, toTeamResponse(t))
}

func writeTeamQuotaError(c *gin.Context, err error) {
	if errors.Is(err, biz.ErrTeamQuotaExceedsTenant) || errors.Is(err, biz.ErrTeamQuotaBelowCommitments) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}

func (h *TeamHandler) Delete(c *gin.Context) {
	err := h.teams.Delete(c.Request.Context(), c.Param("team_id"))
	if errors.Is(err, biz.ErrTeamHasWorkspaces) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *TeamHandler) AddMember(c *gin.Context) {
	if err := h.teams.AddMember(c.Request.Context(), c.Param("team_id"), c.Param("user_id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *TeamHandler) RemoveMember(c *gin.Context) {
	if err := h.teams.RemoveMember(c.Request.Context(), c.Param("team_id"), c.Param("user_id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *TeamHandler) ListMembers(c *gin.Context) {
	ids, err := h.teams.ListMemberIDs(c.Request.Context(), c.Param("team_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"userIds": ids})
}
