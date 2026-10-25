package data

import (
	"context"
	"database/sql"

	"tenant-service/backend/internal/biz"
)

type WorkspaceRepo struct {
	db *sql.DB
}

func NewWorkspaceRepo(d *Data) *WorkspaceRepo {
	return &WorkspaceRepo{db: d.DB}
}

func (r *WorkspaceRepo) Create(ctx context.Context, w *biz.Workspace) error {
	return r.db.QueryRowContext(ctx, `
		INSERT INTO workspaces (tenant_id, team_id, owner_user_id, name, image_key, size, cpu_request, memory_request, gpu_request, storage_request, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, created_at, updated_at`,
		w.TenantID, w.TeamID, w.OwnerUserID, w.Name, w.ImageKey, w.Size, w.CPURequest, w.MemoryRequest, w.GPURequest, w.StorageRequest, string(w.Status),
	).Scan(&w.ID, &w.CreatedAt, &w.UpdatedAt)
}

const workspaceColumns = `id, tenant_id, team_id, owner_user_id, name, image_key, size, cpu_request, memory_request, gpu_request, storage_request, status, k8s_pod_name, k8s_service_name, error_message, created_at, updated_at, stopped_at`

func scanWorkspace(row interface{ Scan(dest ...any) error }) (*biz.Workspace, error) {
	w := &biz.Workspace{}
	var status string
	var podName, svcName, errMsg sql.NullString
	if err := row.Scan(&w.ID, &w.TenantID, &w.TeamID, &w.OwnerUserID, &w.Name, &w.ImageKey, &w.Size,
		&w.CPURequest, &w.MemoryRequest, &w.GPURequest, &w.StorageRequest, &status, &podName, &svcName, &errMsg,
		&w.CreatedAt, &w.UpdatedAt, &w.StoppedAt); err != nil {
		return nil, err
	}
	w.Status = biz.WorkspaceStatus(status)
	w.K8sPodName = podName.String
	w.K8sServiceName = svcName.String
	w.ErrorMessage = errMsg.String
	return w, nil
}

func (r *WorkspaceRepo) Get(ctx context.Context, id string) (*biz.Workspace, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+workspaceColumns+` FROM workspaces WHERE id=$1`, id)
	return scanWorkspace(row)
}

func (r *WorkspaceRepo) list(ctx context.Context, query string, args ...any) ([]*biz.Workspace, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*biz.Workspace
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (r *WorkspaceRepo) ListByOwner(ctx context.Context, ownerUserID string) ([]*biz.Workspace, error) {
	return r.list(ctx, `SELECT `+workspaceColumns+` FROM workspaces WHERE owner_user_id=$1 ORDER BY created_at`, ownerUserID)
}

func (r *WorkspaceRepo) ListByTenant(ctx context.Context, tenantID string) ([]*biz.Workspace, error) {
	return r.list(ctx, `SELECT `+workspaceColumns+` FROM workspaces WHERE tenant_id=$1 ORDER BY created_at`, tenantID)
}

func (r *WorkspaceRepo) ListActiveByOwner(ctx context.Context, ownerUserID string) ([]*biz.Workspace, error) {
	return r.list(ctx, `SELECT `+workspaceColumns+` FROM workspaces WHERE owner_user_id=$1 AND status IN ('pending','running') ORDER BY created_at`, ownerUserID)
}

func (r *WorkspaceRepo) Update(ctx context.Context, w *biz.Workspace) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE workspaces SET status=$1, k8s_pod_name=$2, k8s_service_name=$3, error_message=$4, stopped_at=$5, updated_at=now()
		WHERE id=$6`,
		string(w.Status), nullIfEmpty(w.K8sPodName), nullIfEmpty(w.K8sServiceName), nullIfEmpty(w.ErrorMessage), w.StoppedAt, w.ID,
	)
	return err
}

func nullIfEmpty(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func (r *WorkspaceRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM workspaces WHERE id=$1`, id)
	return err
}
