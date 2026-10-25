package biz

import (
	"context"
	"errors"
	"time"
)

// TokenQuota is a tenant's per-model token budget, enforced by the
// in-cluster AI gateway (agentrouter, running on Envoy Gateway — see
// internal/pkg/k8s/token_quota_provisioner.go) rather than by Tenant
// Service itself. Postgres is the source of truth an admin edits;
// TokenQuotaProvisioner pushes it into the gateway's QuotaPolicy CRD so
// enforcement actually happens at the request path, not just in this UI.
type TokenQuota struct {
	ID         string
	TenantID   string
	ModelName  string
	TokenLimit int64
	Window     string // "daily" | "monthly" — the QuotaPolicy's reset period
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type TokenQuotaRepo interface {
	Create(ctx context.Context, q *TokenQuota) error
	GetByID(ctx context.Context, tenantID, id string) (*TokenQuota, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*TokenQuota, error)
	Update(ctx context.Context, q *TokenQuota) error
	Delete(ctx context.Context, tenantID, id string) error
}

// TokenQuotaProvisioner reconciles a TokenQuota into the AI gateway's
// enforcement config, the same role WorkspaceProvisioner plays for
// workspace Pods — Postgres row in, cluster object out.
type TokenQuotaProvisioner interface {
	EnsureQuota(ctx context.Context, tenantNamespace string, q *TokenQuota) error
	DeleteQuota(ctx context.Context, tenantNamespace string, q *TokenQuota) error
}

var ErrTokenQuotaNotFound = errors.New("token quota not found")

type TokenQuotaBiz struct {
	repo        TokenQuotaRepo
	tenants     TenantRepo
	provisioner TokenQuotaProvisioner
}

func NewTokenQuotaBiz(repo TokenQuotaRepo, tenants TenantRepo, provisioner TokenQuotaProvisioner) *TokenQuotaBiz {
	return &TokenQuotaBiz{repo: repo, tenants: tenants, provisioner: provisioner}
}

func (b *TokenQuotaBiz) namespaceFor(ctx context.Context, tenantID string) (string, error) {
	t, err := b.tenants.Get(ctx, tenantID)
	if err != nil {
		return "", err
	}
	return t.Namespace, nil
}

func (b *TokenQuotaBiz) Create(ctx context.Context, q *TokenQuota) error {
	if err := b.repo.Create(ctx, q); err != nil {
		return err
	}
	ns, err := b.namespaceFor(ctx, q.TenantID)
	if err != nil {
		return err
	}
	return b.provisioner.EnsureQuota(ctx, ns, q)
}

func (b *TokenQuotaBiz) ListByTenant(ctx context.Context, tenantID string) ([]*TokenQuota, error) {
	return b.repo.ListByTenant(ctx, tenantID)
}

// Update fetches the stored row itself (mirroring SizePresetBiz.Update's
// precedent, size_preset.go:70-89) so the caller only has to supply the
// fields actually changing.
func (b *TokenQuotaBiz) Update(ctx context.Context, tenantID, id, modelName string, tokenLimit int64, window string) (*TokenQuota, error) {
	q, err := b.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	q.ModelName, q.TokenLimit, q.Window = modelName, tokenLimit, window
	if err := b.repo.Update(ctx, q); err != nil {
		return nil, err
	}
	ns, err := b.namespaceFor(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if err := b.provisioner.EnsureQuota(ctx, ns, q); err != nil {
		return nil, err
	}
	return q, nil
}

func (b *TokenQuotaBiz) Delete(ctx context.Context, tenantID, id string) error {
	q, err := b.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	ns, err := b.namespaceFor(ctx, tenantID)
	if err != nil {
		return err
	}
	if err := b.provisioner.DeleteQuota(ctx, ns, q); err != nil {
		return err
	}
	return b.repo.Delete(ctx, tenantID, id)
}
