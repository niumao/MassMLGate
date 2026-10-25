package biz

import "context"

// CapacityProvider reports the cluster's actual schedulable node capacity,
// read from the Kubernetes API (not an exporter) so it works even without
// kube-state-metrics deployed.
type CapacityProvider interface {
	GetCapacity(ctx context.Context) (cpuMilli int64, memory int64, gpu int64, err error)
	ListNodes(ctx context.Context) ([]NodeCapacity, error)
}

// NodeCapacity is one cluster node's allocatable capacity, surfaced so the
// dashboard can show per-node numbers instead of only the cluster-wide sum.
type NodeCapacity struct {
	Name        string
	Ready       bool
	Schedulable bool
	CPUMilli    int64
	Memory      int64
	GPU         int64
}

// TenantShare is one tenant's slice of the cluster's distributed capacity,
// read straight from its Postgres quota row (not Kueue/live cluster state)
// — the "Distributed" total already comes from the same source, so
// breaking it down per-tenant is a reshape of data already on hand rather
// than an extra round trip.
type TenantShare struct {
	TenantID      string
	TenantName    string
	QuotaCPUMilli int64
	QuotaMemory   int64
	QuotaGPU      int64
}

// Utilization is cluster-wide CPU/memory/GPU usage from Prometheus. Each Ok
// flag is independent so one missing exporter (e.g. no DCGM yet) doesn't
// blank out the others.
type Utilization struct {
	CPUPct    float64
	CPUOk     bool
	MemoryPct float64
	MemoryOk  bool
	GPUPct    float64
	GPUOk     bool
}

// UtilizationProvider reports live cluster resource usage via Prometheus —
// implemented by pkg/metrics; a nil-safe client returns everything
// unavailable when Prometheus isn't configured.
type UtilizationProvider interface {
	ClusterUtilization(ctx context.Context) Utilization
}

// ResourceSnapshot is the admin dashboard's "capacity vs distributed vs
// free" view: Capacity (actual node capacity, via CapacityProvider),
// Distributed (sum of every tenant's quota — already handed out,
// regardless of whether it's actually in use), Free (Capacity minus
// Distributed, floored at 0 since over-distribution is possible and
// shouldn't show as negative).
type ResourceSnapshot struct {
	CapacityCPUMilli    int64
	CapacityMemory      int64
	CapacityGPU         int64
	DistributedCPUMilli int64
	DistributedMemory   int64
	DistributedGPU      int64
	FreeCPUMilli        int64
	FreeMemory          int64
	FreeGPU             int64
	Utilization         Utilization
	Nodes               []NodeCapacity
	TenantShares        []TenantShare
}

type ClusterBiz struct {
	provisioner ClusterProvisioner
	capacity    CapacityProvider
	utilization UtilizationProvider
	tenants     TenantRepo
}

func NewClusterBiz(provisioner ClusterProvisioner, capacity CapacityProvider, utilization UtilizationProvider, tenants TenantRepo) *ClusterBiz {
	return &ClusterBiz{provisioner: provisioner, capacity: capacity, utilization: utilization, tenants: tenants}
}

func (b *ClusterBiz) EnsureKueueSetup(ctx context.Context, cpu int, memory int64, gpu int) error {
	return b.provisioner.EnsureClusterQueue(ctx, cpu, memory, gpu)
}

func (b *ClusterBiz) GetResourceSnapshot(ctx context.Context) (*ResourceSnapshot, error) {
	cpu, mem, gpu, err := b.capacity.GetCapacity(ctx)
	if err != nil {
		return nil, err
	}
	tenants, err := b.tenants.List(ctx)
	if err != nil {
		return nil, err
	}
	snap := &ResourceSnapshot{CapacityCPUMilli: cpu, CapacityMemory: mem, CapacityGPU: gpu}
	for _, t := range tenants {
		snap.DistributedCPUMilli += int64(t.QuotaCPU)
		snap.DistributedMemory += t.QuotaMemory
		snap.DistributedGPU += int64(t.QuotaGPU)
		snap.TenantShares = append(snap.TenantShares, TenantShare{
			TenantID:      t.ID,
			TenantName:    t.Name,
			QuotaCPUMilli: int64(t.QuotaCPU),
			QuotaMemory:   t.QuotaMemory,
			QuotaGPU:      int64(t.QuotaGPU),
		})
	}
	snap.FreeCPUMilli = max0(snap.CapacityCPUMilli - snap.DistributedCPUMilli)
	snap.FreeMemory = max0(snap.CapacityMemory - snap.DistributedMemory)
	snap.FreeGPU = max0(snap.CapacityGPU - snap.DistributedGPU)
	snap.Utilization = b.utilization.ClusterUtilization(ctx)
	nodes, err := b.capacity.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	snap.Nodes = nodes
	return snap, nil
}

func max0(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}
