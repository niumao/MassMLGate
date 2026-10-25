package data

import (
	"context"
	"database/sql"
	"errors"

	"tenant-service/backend/internal/biz"
)

type SizePresetRepo struct {
	db *sql.DB
}

func NewSizePresetRepo(d *Data) *SizePresetRepo {
	return &SizePresetRepo{db: d.DB}
}

func (r *SizePresetRepo) Create(ctx context.Context, p *biz.SizePreset) error {
	return r.db.QueryRowContext(ctx, `
		INSERT INTO tenant_size_presets (tenant_id, name, cpu_request, memory_request, gpu_request, storage)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at`,
		p.TenantID, p.Name, p.CPURequest, p.MemoryRequest, p.GPURequest, p.Storage,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
}

const sizePresetColumns = `id, tenant_id, name, cpu_request, memory_request, gpu_request, storage, created_at, updated_at`

func (r *SizePresetRepo) Get(ctx context.Context, tenantID, name string) (*biz.SizePreset, error) {
	p := &biz.SizePreset{}
	err := r.db.QueryRowContext(ctx, `SELECT `+sizePresetColumns+` FROM tenant_size_presets WHERE tenant_id=$1 AND name=$2`, tenantID, name).
		Scan(&p.ID, &p.TenantID, &p.Name, &p.CPURequest, &p.MemoryRequest, &p.GPURequest, &p.Storage, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, biz.ErrSizePresetNotFound
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (r *SizePresetRepo) GetByID(ctx context.Context, tenantID, id string) (*biz.SizePreset, error) {
	p := &biz.SizePreset{}
	err := r.db.QueryRowContext(ctx, `SELECT `+sizePresetColumns+` FROM tenant_size_presets WHERE tenant_id=$1 AND id=$2`, tenantID, id).
		Scan(&p.ID, &p.TenantID, &p.Name, &p.CPURequest, &p.MemoryRequest, &p.GPURequest, &p.Storage, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, biz.ErrSizePresetNotFound
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (r *SizePresetRepo) ListByTenant(ctx context.Context, tenantID string) ([]*biz.SizePreset, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+sizePresetColumns+` FROM tenant_size_presets WHERE tenant_id=$1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*biz.SizePreset
	for rows.Next() {
		p := &biz.SizePreset{}
		if err := rows.Scan(&p.ID, &p.TenantID, &p.Name, &p.CPURequest, &p.MemoryRequest, &p.GPURequest, &p.Storage, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *SizePresetRepo) Update(ctx context.Context, p *biz.SizePreset) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE tenant_size_presets SET name=$1, cpu_request=$2, memory_request=$3, gpu_request=$4, storage=$5, updated_at=now()
		WHERE id=$6 AND tenant_id=$7`,
		p.Name, p.CPURequest, p.MemoryRequest, p.GPURequest, p.Storage, p.ID, p.TenantID,
	)
	return err
}

func (r *SizePresetRepo) Delete(ctx context.Context, tenantID, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM tenant_size_presets WHERE tenant_id=$1 AND id=$2`, tenantID, id)
	return err
}

func (r *SizePresetRepo) CountWorkspacesUsing(ctx context.Context, tenantID, name string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workspaces WHERE tenant_id=$1 AND size=$2`, tenantID, name,
	).Scan(&count)
	return count, err
}
