package data

import (
	"context"
	"database/sql"
	"errors"

	"tenant-service/backend/internal/biz"
)

type TokenQuotaRepo struct {
	db *sql.DB
}

func NewTokenQuotaRepo(d *Data) *TokenQuotaRepo {
	return &TokenQuotaRepo{db: d.DB}
}

func (r *TokenQuotaRepo) Create(ctx context.Context, q *biz.TokenQuota) error {
	return r.db.QueryRowContext(ctx, `
		INSERT INTO token_quotas (tenant_id, model_name, token_limit, "window")
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at`,
		q.TenantID, q.ModelName, q.TokenLimit, q.Window,
	).Scan(&q.ID, &q.CreatedAt, &q.UpdatedAt)
}

const tokenQuotaColumns = `id, tenant_id, model_name, token_limit, "window", created_at, updated_at`

func (r *TokenQuotaRepo) GetByID(ctx context.Context, tenantID, id string) (*biz.TokenQuota, error) {
	q := &biz.TokenQuota{}
	err := r.db.QueryRowContext(ctx, `SELECT `+tokenQuotaColumns+` FROM token_quotas WHERE tenant_id=$1 AND id=$2`, tenantID, id).
		Scan(&q.ID, &q.TenantID, &q.ModelName, &q.TokenLimit, &q.Window, &q.CreatedAt, &q.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, biz.ErrTokenQuotaNotFound
	}
	if err != nil {
		return nil, err
	}
	return q, nil
}

func (r *TokenQuotaRepo) ListByTenant(ctx context.Context, tenantID string) ([]*biz.TokenQuota, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+tokenQuotaColumns+` FROM token_quotas WHERE tenant_id=$1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*biz.TokenQuota
	for rows.Next() {
		q := &biz.TokenQuota{}
		if err := rows.Scan(&q.ID, &q.TenantID, &q.ModelName, &q.TokenLimit, &q.Window, &q.CreatedAt, &q.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (r *TokenQuotaRepo) Update(ctx context.Context, q *biz.TokenQuota) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE token_quotas SET model_name=$1, token_limit=$2, "window"=$3, updated_at=now()
		WHERE id=$4 AND tenant_id=$5`,
		q.ModelName, q.TokenLimit, q.Window, q.ID, q.TenantID,
	)
	return err
}

func (r *TokenQuotaRepo) Delete(ctx context.Context, tenantID, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM token_quotas WHERE tenant_id=$1 AND id=$2`, tenantID, id)
	return err
}
