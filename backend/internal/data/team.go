package data

import (
	"context"
	"database/sql"

	"tenant-service/backend/internal/biz"
)

type TeamRepo struct {
	db *sql.DB
}

func NewTeamRepo(d *Data) *TeamRepo {
	return &TeamRepo{db: d.DB}
}

func (r *TeamRepo) Create(ctx context.Context, t *biz.Team) error {
	return r.db.QueryRowContext(ctx, `
		INSERT INTO teams (tenant_id, name, quota_cpu, quota_memory, quota_gpu)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at`,
		t.TenantID, t.Name, t.QuotaCPU, t.QuotaMemory, t.QuotaGPU,
	).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
}

const teamColumns = `id, tenant_id, name, quota_cpu, quota_memory, quota_gpu, created_at, updated_at`

func (r *TeamRepo) Get(ctx context.Context, id string) (*biz.Team, error) {
	t := &biz.Team{}
	err := r.db.QueryRowContext(ctx, `SELECT `+teamColumns+` FROM teams WHERE id=$1`, id).
		Scan(&t.ID, &t.TenantID, &t.Name, &t.QuotaCPU, &t.QuotaMemory, &t.QuotaGPU, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (r *TeamRepo) ListByTenant(ctx context.Context, tenantID string) ([]*biz.Team, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+teamColumns+` FROM teams WHERE tenant_id=$1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*biz.Team
	for rows.Next() {
		t := &biz.Team{}
		if err := rows.Scan(&t.ID, &t.TenantID, &t.Name, &t.QuotaCPU, &t.QuotaMemory, &t.QuotaGPU, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *TeamRepo) ListByMember(ctx context.Context, userID string) ([]*biz.Team, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+teamColumns+` FROM teams t
		JOIN team_members tm ON tm.team_id = t.id
		WHERE tm.user_id = $1 ORDER BY t.created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*biz.Team
	for rows.Next() {
		t := &biz.Team{}
		if err := rows.Scan(&t.ID, &t.TenantID, &t.Name, &t.QuotaCPU, &t.QuotaMemory, &t.QuotaGPU, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *TeamRepo) Update(ctx context.Context, t *biz.Team) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE teams SET name=$1, quota_cpu=$2, quota_memory=$3, quota_gpu=$4, updated_at=now()
		WHERE id=$5`,
		t.Name, t.QuotaCPU, t.QuotaMemory, t.QuotaGPU, t.ID,
	)
	return err
}

func (r *TeamRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM teams WHERE id=$1`, id)
	return err
}

func (r *TeamRepo) HasWorkspaces(ctx context.Context, teamID string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspaces WHERE team_id=$1`, teamID).Scan(&count)
	return count > 0, err
}

func (r *TeamRepo) AddMember(ctx context.Context, teamID, userID string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO team_members (team_id, user_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, teamID, userID)
	return err
}

func (r *TeamRepo) RemoveMember(ctx context.Context, teamID, userID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM team_members WHERE team_id=$1 AND user_id=$2`, teamID, userID)
	return err
}

func (r *TeamRepo) ListMemberIDs(ctx context.Context, teamID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT user_id FROM team_members WHERE team_id=$1`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *TeamRepo) IsMember(ctx context.Context, teamID, userID string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM team_members WHERE team_id=$1 AND user_id=$2)`, teamID, userID,
	).Scan(&exists)
	return exists, err
}

// SumActiveWorkspaceRequest always returns zero in Phase 1 since the
// workspaces table isn't populated until the Phase 2 K8s lifecycle lands;
// the query itself is written now so TeamBiz's quota check is already
// correct once workspaces exist.
func (r *TeamRepo) SumActiveWorkspaceRequest(ctx context.Context, teamID string) (cpu int, memory int64, gpu int, err error) {
	err = r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(cpu_request),0), COALESCE(SUM(memory_request),0), COALESCE(SUM(gpu_request),0)
		FROM workspaces WHERE team_id=$1 AND status IN ('pending','running')`, teamID,
	).Scan(&cpu, &memory, &gpu)
	return
}
