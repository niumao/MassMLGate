package data

import (
	"context"
	"database/sql"

	"tenant-service/backend/internal/biz"
)

type TenantRepo struct {
	db *sql.DB
}

func NewTenantRepo(d *Data) *TenantRepo {
	return &TenantRepo{db: d.DB}
}

func (r *TenantRepo) Create(ctx context.Context, t *biz.Tenant) error {
	return r.db.QueryRowContext(ctx, `
		INSERT INTO tenants (name, namespace, quota_cpu, quota_memory, quota_gpu, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at`,
		t.Name, t.Namespace, t.QuotaCPU, t.QuotaMemory, t.QuotaGPU, t.Status,
	).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
}

func (r *TenantRepo) Get(ctx context.Context, id string) (*biz.Tenant, error) {
	t := &biz.Tenant{}
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name, namespace, quota_cpu, quota_memory, quota_gpu, status, created_at, updated_at
		FROM tenants WHERE id = $1`, id,
	).Scan(&t.ID, &t.Name, &t.Namespace, &t.QuotaCPU, &t.QuotaMemory, &t.QuotaGPU, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (r *TenantRepo) List(ctx context.Context) ([]*biz.Tenant, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, namespace, quota_cpu, quota_memory, quota_gpu, status, created_at, updated_at
		FROM tenants ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*biz.Tenant
	for rows.Next() {
		t := &biz.Tenant{}
		if err := rows.Scan(&t.ID, &t.Name, &t.Namespace, &t.QuotaCPU, &t.QuotaMemory, &t.QuotaGPU, &t.Status, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *TenantRepo) Update(ctx context.Context, t *biz.Tenant) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE tenants SET name=$1, quota_cpu=$2, quota_memory=$3, quota_gpu=$4, status=$5, updated_at=now()
		WHERE id=$6`,
		t.Name, t.QuotaCPU, t.QuotaMemory, t.QuotaGPU, t.Status, t.ID,
	)
	return err
}

func (r *TenantRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM tenants WHERE id=$1`, id)
	return err
}

func (r *TenantRepo) SumTeamQuota(ctx context.Context, tenantID string) (cpu int, memory int64, gpu int, err error) {
	err = r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(quota_cpu),0), COALESCE(SUM(quota_memory),0), COALESCE(SUM(quota_gpu),0)
		FROM teams WHERE tenant_id = $1`, tenantID,
	).Scan(&cpu, &memory, &gpu)
	return
}
