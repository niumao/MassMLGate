package biz

import (
	"context"
	"errors"
	"time"
)

type Tenant struct {
	ID          string
	Name        string
	Namespace   string
	QuotaCPU    int
	QuotaMemory int64
	QuotaGPU    int
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type TenantRepo interface {
	Create(ctx context.Context, t *Tenant) error
	Get(ctx context.Context, id string) (*Tenant, error)
	List(ctx context.Context) ([]*Tenant, error)
	Update(ctx context.Context, t *Tenant) error
	Delete(ctx context.Context, id string) error
	SumTeamQuota(ctx context.Context, tenantID string) (cpu int, memory int64, gpu int, err error)
}

var ErrQuotaBelowTeamCommitments = errors.New("tenant quota cannot be lower than the sum of its teams' quotas")

type TenantBiz struct {
	repo        TenantRepo
	provisioner Provisioner
}

func NewTenantBiz(repo TenantRepo, provisioner Provisioner) *TenantBiz {
	return &TenantBiz{repo: repo, provisioner: provisioner}
}

func (b *TenantBiz) Create(ctx context.Context, t *Tenant) error {
	t.Status = "active"
	if err := b.repo.Create(ctx, t); err != nil {
		return err
	}
	if err := b.provisioner.EnsureTenant(ctx, t); err != nil {
		t.Status = "provisioning_failed"
		_ = b.repo.Update(ctx, t)
		return err
	}
	return nil
}

func (b *TenantBiz) List(ctx context.Context) ([]*Tenant, error) {
	return b.repo.List(ctx)
}

func (b *TenantBiz) Get(ctx context.Context, id string) (*Tenant, error) {
	return b.repo.Get(ctx, id)
}

// UpdateQuota rejects the change if it would fall below the sum of the
// tenant's existing team quotas (see design spec's Data Model section).
func (b *TenantBiz) UpdateQuota(ctx context.Context, id string, cpu int, memory int64, gpu int) (*Tenant, error) {
	t, err := b.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	teamCPU, teamMemory, teamGPU, err := b.repo.SumTeamQuota(ctx, id)
	if err != nil {
		return nil, err
	}
	if cpu < teamCPU || memory < teamMemory || gpu < teamGPU {
		return nil, ErrQuotaBelowTeamCommitments
	}
	t.QuotaCPU, t.QuotaMemory, t.QuotaGPU = cpu, memory, gpu
	if err := b.repo.Update(ctx, t); err != nil {
		return nil, err
	}
	if err := b.provisioner.EnsureTenant(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (b *TenantBiz) Delete(ctx context.Context, id string) error {
	t, err := b.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := b.provisioner.DeleteTenant(ctx, t); err != nil {
		return err
	}
	return b.repo.Delete(ctx, id)
}
