package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"tenant-service/backend/internal/biz"
)

// ClusterHandler backs the System Admin "cluster setup" action — creating/
// updating the shared Kueue ResourceFlavor + ClusterQueue from the admin
// page, instead of a human applying deploy/kueue-bootstrap.yaml by hand.
type ClusterHandler struct {
	cluster *biz.ClusterBiz
}

func NewClusterHandler(cluster *biz.ClusterBiz) *ClusterHandler {
	return &ClusterHandler{cluster: cluster}
}

type kueueSetupRequest struct {
	QuotaCPU    int   `json:"quotaCpu" binding:"required"`
	QuotaMemory int64 `json:"quotaMemory" binding:"required"`
	QuotaGPU    int   `json:"quotaGpu"`
}

func (h *ClusterHandler) KueueSetup(c *gin.Context) {
	var req kueueSetupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.cluster.EnsureKueueSetup(c.Request.Context(), req.QuotaCPU, req.QuotaMemory, req.QuotaGPU); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
