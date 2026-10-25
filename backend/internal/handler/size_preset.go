package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"tenant-service/backend/internal/biz"
)

type SizePresetHandler struct {
	presets *biz.SizePresetBiz
}

func NewSizePresetHandler(presets *biz.SizePresetBiz) *SizePresetHandler {
	return &SizePresetHandler{presets: presets}
}

type sizePresetResponse struct {
	ID            string `json:"id"`
	TenantID      string `json:"tenantId"`
	Name          string `json:"name"`
	CPURequest    int    `json:"cpuRequest"`
	MemoryRequest int64  `json:"memoryRequest"`
	GPURequest    int    `json:"gpuRequest"`
	Storage       int64  `json:"storage"`
}

func toSizePresetResponse(p *biz.SizePreset) sizePresetResponse {
	return sizePresetResponse{
		ID: p.ID, TenantID: p.TenantID, Name: p.Name,
		CPURequest: p.CPURequest, MemoryRequest: p.MemoryRequest, GPURequest: p.GPURequest, Storage: p.Storage,
	}
}

// CPURequest/MemoryRequest/GPURequest/Storage use `binding:"gte=0"`, not
// `binding:"required"` — same reasoning as createTeamRequest (team.go:34-39):
// `required` rejects a legitimate 0 (e.g. a GPU-free preset needs
// GPURequest: 0 to bind), but `gte=0` still rejects negative values, which
// is the actual guard this spec needs (see biz/size_preset.go's DB-level
// CHECK constraint — this is the same rule enforced one layer up, for a
// faster/clearer 400 instead of a raw Postgres constraint-violation error).
type createSizePresetRequest struct {
	Name          string `json:"name" binding:"required"`
	CPURequest    int    `json:"cpuRequest" binding:"gte=0"`
	MemoryRequest int64  `json:"memoryRequest" binding:"gte=0"`
	GPURequest    int    `json:"gpuRequest" binding:"gte=0"`
	Storage       int64  `json:"storage" binding:"gte=0"`
}

func (h *SizePresetHandler) Create(c *gin.Context) {
	var req createSizePresetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p := &biz.SizePreset{
		TenantID: callerTenantID(c), Name: req.Name,
		CPURequest: req.CPURequest, MemoryRequest: req.MemoryRequest, GPURequest: req.GPURequest, Storage: req.Storage,
	}
	if err := h.presets.Create(c.Request.Context(), p); err != nil {
		writeSizePresetError(c, err)
		return
	}
	c.JSON(http.StatusCreated, toSizePresetResponse(p))
}

func (h *SizePresetHandler) List(c *gin.Context) {
	list, err := h.presets.ListByTenant(c.Request.Context(), callerTenantID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	resp := make([]sizePresetResponse, 0, len(list))
	for _, p := range list {
		resp = append(resp, toSizePresetResponse(p))
	}
	c.JSON(http.StatusOK, resp)
}

type updateSizePresetRequest struct {
	Name          string `json:"name" binding:"required"`
	CPURequest    int    `json:"cpuRequest" binding:"gte=0"`
	MemoryRequest int64  `json:"memoryRequest" binding:"gte=0"`
	GPURequest    int    `json:"gpuRequest" binding:"gte=0"`
	Storage       int64  `json:"storage" binding:"gte=0"`
}

func (h *SizePresetHandler) Update(c *gin.Context) {
	var req updateSizePresetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, err := h.presets.Update(c.Request.Context(), callerTenantID(c), c.Param("id"), req.Name, req.CPURequest, req.MemoryRequest, req.GPURequest, req.Storage)
	if err != nil {
		writeSizePresetError(c, err)
		return
	}
	c.JSON(http.StatusOK, toSizePresetResponse(p))
}

func (h *SizePresetHandler) Delete(c *gin.Context) {
	if err := h.presets.Delete(c.Request.Context(), callerTenantID(c), c.Param("id")); err != nil {
		writeSizePresetError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// writeSizePresetError is this handler's own mapper (same role as
// writeTeamQuotaError, team.go:138-144) — deliberately separate from
// writeWorkspaceError's unrelated 400 mapping of the same ErrSizePresetNotFound
// sentinel in the workspace create/start context. This handler maps "not
// found" to 404 (covers both "no such id" and "id belongs to another
// tenant" — GetByID doesn't distinguish the two, so neither does this).
func writeSizePresetError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, biz.ErrSizePresetNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, biz.ErrSizePresetInUse):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
