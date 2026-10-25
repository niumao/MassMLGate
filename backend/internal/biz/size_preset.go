package biz

import (
	"context"
	"errors"
	"time"
)

// SizePreset is a tenant's own named CPU/Memory/GPU/Storage request for a
// workspace size — replaces the old global, identical-for-every-tenant
// biz.SizePreset/SizePresets map (see workspace.go). Storage backs the
// workspace's persistent "save" volume, sized separately from Memory since
// a notebook's saved files and its RAM usage are unrelated quantities.
type SizePreset struct {
	ID            string
	TenantID      string
	Name          string
	CPURequest    int
	MemoryRequest int64
	GPURequest    int
	Storage       int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type SizePresetRepo interface {
	Create(ctx context.Context, p *SizePreset) error
	// Get looks up by name — used by WorkspaceBiz.Create/.Start to resolve a
	// workspace's requested Size against its tenant's catalog.
	Get(ctx context.Context, tenantID, name string) (*SizePreset, error)
	// GetByID looks up by id, tenant-checked — used internally by Update/
	// Delete below; not its own HTTP endpoint (see handler/size_preset.go).
	GetByID(ctx context.Context, tenantID, id string) (*SizePreset, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*SizePreset, error)
	Update(ctx context.Context, p *SizePreset) error
	Delete(ctx context.Context, tenantID, id string) error
	// CountWorkspacesUsing counts workspaces (any status) in tenantID whose
	// Size equals name — the in-use guard for Update (rename only)/Delete.
	CountWorkspacesUsing(ctx context.Context, tenantID, name string) (int, error)
}

var (
	ErrSizePresetInUse    = errors.New("size preset is in use by an existing workspace")
	ErrSizePresetNotFound = errors.New("size preset not found")
)

type SizePresetBiz struct {
	repo SizePresetRepo
}

func NewSizePresetBiz(repo SizePresetRepo) *SizePresetBiz {
	return &SizePresetBiz{repo: repo}
}

func (b *SizePresetBiz) Create(ctx context.Context, p *SizePreset) error {
	return b.repo.Create(ctx, p)
}

func (b *SizePresetBiz) ListByTenant(ctx context.Context, tenantID string) ([]*SizePreset, error) {
	return b.repo.ListByTenant(ctx, tenantID)
}

// Update fetches the stored row itself (mirroring TeamBiz.UpdateQuota's
// fetch-then-modify precedent, team.go:87-102) rather than requiring the
// caller to supply it. The in-use guard only applies when name is an actual
// rename (p.Name != name) — a same-name value edit is always allowed, even
// on a preset already referenced by a workspace, since existing workspaces
// keep their own already-snapshotted resource request regardless (see
// Workspace.CPURequest/MemoryRequest/GPURequest, workspace.go:159).
func (b *SizePresetBiz) Update(ctx context.Context, tenantID, id, name string, cpu int, memory int64, gpu int, storage int64) (*SizePreset, error) {
	p, err := b.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if p.Name != name {
		n, err := b.repo.CountWorkspacesUsing(ctx, tenantID, p.Name)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, ErrSizePresetInUse
		}
	}
	p.Name, p.CPURequest, p.MemoryRequest, p.GPURequest, p.Storage = name, cpu, memory, gpu, storage
	if err := b.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Delete is keyed by id, but CountWorkspacesUsing is keyed by name, so it
// fetches the row first (same as Update) to resolve id -> name.
func (b *SizePresetBiz) Delete(ctx context.Context, tenantID, id string) error {
	p, err := b.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	n, err := b.repo.CountWorkspacesUsing(ctx, tenantID, p.Name)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrSizePresetInUse
	}
	return b.repo.Delete(ctx, tenantID, id)
}
