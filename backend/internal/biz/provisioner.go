package biz

import "context"

// Provisioner reconciles a tenant's K8s namespace, Kueue LocalQueue, and
// ResourceQuota to match its quota fields. Implemented by internal/pkg/k8s;
// a no-op implementation is used when K8s isn't available (local dev).
type Provisioner interface {
	EnsureTenant(ctx context.Context, t *Tenant) error
	DeleteTenant(ctx context.Context, t *Tenant) error
}

// ClusterProvisioner creates/updates the cluster-wide Kueue ResourceFlavor
// and ClusterQueue every tenant's per-namespace LocalQueue binds to. Unlike
// per-tenant provisioning, this is a System Admin-triggered one-time (or
// repeatable) cluster bootstrap action, not run automatically on tenant
// CRUD — see POST /api/v1/cluster/kueue-setup.
type ClusterProvisioner interface {
	EnsureClusterQueue(ctx context.Context, cpu int, memory int64, gpu int) error
}
