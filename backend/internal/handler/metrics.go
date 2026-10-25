package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"tenant-service/backend/internal/biz"
)

// MetricsHandler serves the dashboard KPI tiles. The count-based KPIs are
// queried from Postgres; the resource snapshot (capacity/distributed/free,
// live utilization) comes from ClusterBiz — node capacity via the K8s API,
// utilization via Prometheus (gracefully "unavailable" if unconfigured).
type MetricsHandler struct {
	tenants *biz.TenantBiz
	users   *biz.UserBiz
	teams   *biz.TeamBiz
	cluster *biz.ClusterBiz
}

func NewMetricsHandler(tenants *biz.TenantBiz, users *biz.UserBiz, teams *biz.TeamBiz, cluster *biz.ClusterBiz) *MetricsHandler {
	return &MetricsHandler{tenants: tenants, users: users, teams: teams, cluster: cluster}
}

func (h *MetricsHandler) Cluster(c *gin.Context) {
	tenants, err := h.tenants.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	users, err := h.users.ListAll(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	failedProvisioning := 0
	for _, t := range tenants {
		if t.Status == "provisioning_failed" {
			failedProvisioning++
		}
	}
	snapshot, err := h.cluster.GetResourceSnapshot(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"tenantCount":        len(tenants),
		"userCount":          len(users),
		"provisioningFailed": failedProvisioning,
		"resources": gin.H{
			"cpu":    resourceBreakdown(snapshot.CapacityCPUMilli, snapshot.DistributedCPUMilli, snapshot.FreeCPUMilli),
			"memory": resourceBreakdown(snapshot.CapacityMemory, snapshot.DistributedMemory, snapshot.FreeMemory),
			"gpu":    resourceBreakdown(snapshot.CapacityGPU, snapshot.DistributedGPU, snapshot.FreeGPU),
		},
		"utilization": gin.H{
			"cpu":    utilizationField(snapshot.Utilization.CPUPct, snapshot.Utilization.CPUOk),
			"memory": utilizationField(snapshot.Utilization.MemoryPct, snapshot.Utilization.MemoryOk),
			"gpu":    utilizationField(snapshot.Utilization.GPUPct, snapshot.Utilization.GPUOk),
		},
		"nodes":        nodeRows(snapshot.Nodes),
		"tenantShares": tenantShareRows(snapshot.TenantShares),
	})
}

func resourceBreakdown(capacity, distributed, free int64) gin.H {
	return gin.H{"capacity": capacity, "distributed": distributed, "free": free}
}

func utilizationField(pct float64, ok bool) gin.H {
	if !ok {
		return gin.H{"available": false}
	}
	return gin.H{"available": true, "pct": pct}
}

// nodeRows renders per-node capacity, unfiltered — including NotReady or
// cordoned nodes — so the dashboard shows the cluster as it actually is
// rather than only the nodes counted toward the cluster-wide total.
func nodeRows(nodes []biz.NodeCapacity) []gin.H {
	rows := make([]gin.H, len(nodes))
	for i, n := range nodes {
		rows[i] = gin.H{
			"name":        n.Name,
			"ready":       n.Ready,
			"schedulable": n.Schedulable,
			"cpuMilli":    n.CPUMilli,
			"memory":      n.Memory,
			"gpu":         n.GPU,
		}
	}
	return rows
}

// tenantShareRows is each tenant's quota straight from its Postgres row —
// the same source already summed into "Distributed" above, reshaped
// per-tenant instead of an extra query against Kueue/live cluster state.
func tenantShareRows(shares []biz.TenantShare) []gin.H {
	rows := make([]gin.H, len(shares))
	for i, s := range shares {
		rows[i] = gin.H{
			"tenantId":    s.TenantID,
			"tenantName":  s.TenantName,
			"quotaCpu":    s.QuotaCPUMilli,
			"quotaMemory": s.QuotaMemory,
			"quotaGpu":    s.QuotaGPU,
		}
	}
	return rows
}

func (h *MetricsHandler) Tenant(c *gin.Context) {
	tenantID := callerTenantID(c)
	users, err := h.users.ListByTenant(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	teams, err := h.teams.ListByTenant(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	tenant, err := h.tenants.Get(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var usedCPU, usedGPU int
	var usedMemory int64
	for _, t := range teams {
		usedCPU += t.QuotaCPU
		usedMemory += t.QuotaMemory
		usedGPU += t.QuotaGPU
	}
	c.JSON(http.StatusOK, gin.H{
		"teamCount":        len(teams),
		"userCount":        len(users),
		"quotaCpu":         gin.H{"used": usedCPU, "total": tenant.QuotaCPU},
		"quotaMemory":      gin.H{"used": usedMemory, "total": tenant.QuotaMemory},
		"quotaGpu":         gin.H{"used": usedGPU, "total": tenant.QuotaGPU},
		"activeWorkspaces": 0,
	})
}
