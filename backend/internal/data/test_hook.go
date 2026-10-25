package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"tenant-service/backend/internal/biz"
)

type TestHookRepo struct {
	db *sql.DB
}

func NewTestHookRepo(d *Data) *TestHookRepo {
	return &TestHookRepo{db: d.DB}
}

const testHookColumns = `id, tenant_id, workspace_id, name, token_hash, test_image, test_command, image_pull_secret, created_at, updated_at, revoked_at, last_triggered_at`

func (r *TestHookRepo) Create(ctx context.Context, h *biz.TestHook) error {
	cmd, err := json.Marshal(h.TestCommand)
	if err != nil {
		return err
	}
	return r.db.QueryRowContext(ctx, `
		INSERT INTO test_hooks (tenant_id, workspace_id, name, token_hash, test_image, test_command, image_pull_secret)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at`,
		h.TenantID, h.WorkspaceID, h.Name, h.TokenHash, h.TestImage, cmd, nullable(h.ImagePullSecret),
	).Scan(&h.ID, &h.CreatedAt, &h.UpdatedAt)
}

func scanTestHook(scan func(dest ...any) error) (*biz.TestHook, error) {
	h := &biz.TestHook{}
	var cmd []byte
	var imagePullSecret sql.NullString
	if err := scan(&h.ID, &h.TenantID, &h.WorkspaceID, &h.Name, &h.TokenHash, &h.TestImage, &cmd,
		&imagePullSecret, &h.CreatedAt, &h.UpdatedAt, &h.RevokedAt, &h.LastTriggeredAt); err != nil {
		return nil, err
	}
	h.ImagePullSecret = imagePullSecret.String
	if len(cmd) > 0 {
		if err := json.Unmarshal(cmd, &h.TestCommand); err != nil {
			return nil, err
		}
	}
	return h, nil
}

func (r *TestHookRepo) GetByID(ctx context.Context, tenantID, id string) (*biz.TestHook, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+testHookColumns+` FROM test_hooks WHERE tenant_id=$1 AND id=$2`, tenantID, id)
	h, err := scanTestHook(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, biz.ErrTestHookNotFound
	}
	return h, err
}

func (r *TestHookRepo) GetByTokenHash(ctx context.Context, tokenHash string) (*biz.TestHook, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+testHookColumns+` FROM test_hooks WHERE token_hash=$1`, tokenHash)
	h, err := scanTestHook(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, biz.ErrTestHookNotFound
	}
	return h, err
}

func (r *TestHookRepo) ListByWorkspace(ctx context.Context, workspaceID string) ([]*biz.TestHook, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+testHookColumns+` FROM test_hooks WHERE workspace_id=$1 ORDER BY created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*biz.TestHook
	for rows.Next() {
		h, err := scanTestHook(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r *TestHookRepo) Revoke(ctx context.Context, tenantID, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE test_hooks SET revoked_at = now() WHERE tenant_id=$1 AND id=$2 AND revoked_at IS NULL`, tenantID, id)
	return err
}

func (r *TestHookRepo) Touch(ctx context.Context, id string, when time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE test_hooks SET last_triggered_at=$1 WHERE id=$2`, when, id)
	return err
}

func nullable(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
