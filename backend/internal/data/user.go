package data

import (
	"context"
	"database/sql"

	"tenant-service/backend/internal/biz"
)

type UserRepo struct {
	db *sql.DB
}

func NewUserRepo(d *Data) *UserRepo {
	return &UserRepo{db: d.DB}
}

func (r *UserRepo) Create(ctx context.Context, u *biz.User) error {
	return r.db.QueryRowContext(ctx, `
		INSERT INTO users (tenant_id, email, password_hash, display_name, role, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at`,
		u.TenantID, u.Email, u.PasswordHash, u.DisplayName, string(u.Role), u.Status,
	).Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)
}

func (r *UserRepo) scanOne(row *sql.Row) (*biz.User, error) {
	u := &biz.User{}
	var role string
	if err := row.Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.DisplayName, &role, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}
	u.Role = biz.Role(role)
	return u, nil
}

const userColumns = `id, tenant_id, email, password_hash, display_name, role, status, created_at, updated_at`

func (r *UserRepo) Get(ctx context.Context, id string) (*biz.User, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1`, id)
	return r.scanOne(row)
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*biz.User, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE email=$1`, email)
	return r.scanOne(row)
}

func (r *UserRepo) ListByTenant(ctx context.Context, tenantID string) ([]*biz.User, error) {
	return r.list(ctx, `SELECT `+userColumns+` FROM users WHERE tenant_id=$1 ORDER BY created_at`, tenantID)
}

func (r *UserRepo) ListAll(ctx context.Context) ([]*biz.User, error) {
	return r.list(ctx, `SELECT `+userColumns+` FROM users ORDER BY created_at`)
}

func (r *UserRepo) list(ctx context.Context, query string, args ...any) ([]*biz.User, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*biz.User
	for rows.Next() {
		u := &biz.User{}
		var role string
		if err := rows.Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.DisplayName, &role, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		u.Role = biz.Role(role)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (r *UserRepo) Update(ctx context.Context, u *biz.User) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users SET email=$1, display_name=$2, role=$3, status=$4, updated_at=now()
		WHERE id=$5`,
		u.Email, u.DisplayName, string(u.Role), u.Status, u.ID,
	)
	return err
}

func (r *UserRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, id)
	return err
}
