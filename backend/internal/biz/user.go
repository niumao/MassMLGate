package biz

import (
	"context"
	"errors"
	"time"
)

type Role string

const (
	RoleSystemAdmin Role = "system_admin"
	RoleTenantAdmin Role = "tenant_admin"
	RoleMember      Role = "member"
)

type User struct {
	ID           string
	TenantID     *string
	Email        string
	PasswordHash string
	DisplayName  string
	Role         Role
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type UserRepo interface {
	Create(ctx context.Context, u *User) error
	Get(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*User, error)
	ListAll(ctx context.Context) ([]*User, error)
	Update(ctx context.Context, u *User) error
	Delete(ctx context.Context, id string) error
}

var ErrCannotEscalateRole = errors.New("tenant admins may only create/update users with role=member or role=tenant_admin")

type UserBiz struct {
	repo       UserRepo
	policy     PolicySync
	workspaces *WorkspaceBiz
}

func NewUserBiz(repo UserRepo, policy PolicySync, workspaces *WorkspaceBiz) *UserBiz {
	return &UserBiz{repo: repo, policy: policy, workspaces: workspaces}
}

// Create is called by a tenant_admin acting within their own tenant; the
// handler sets u.TenantID to the caller's tenant before calling this.
func (b *UserBiz) Create(ctx context.Context, u *User) error {
	if u.Role == RoleSystemAdmin {
		return ErrCannotEscalateRole
	}
	if err := b.repo.Create(ctx, u); err != nil {
		return err
	}
	return b.policy.SyncUser(ctx, u)
}

func (b *UserBiz) ListByTenant(ctx context.Context, tenantID string) ([]*User, error) {
	return b.repo.ListByTenant(ctx, tenantID)
}

func (b *UserBiz) ListAll(ctx context.Context) ([]*User, error) {
	return b.repo.ListAll(ctx)
}

func (b *UserBiz) Get(ctx context.Context, id string) (*User, error) {
	return b.repo.Get(ctx, id)
}

func (b *UserBiz) GetByEmail(ctx context.Context, email string) (*User, error) {
	return b.repo.GetByEmail(ctx, email)
}

func (b *UserBiz) Update(ctx context.Context, u *User) error {
	if u.Role == RoleSystemAdmin {
		return ErrCannotEscalateRole
	}
	if err := b.repo.Update(ctx, u); err != nil {
		return err
	}
	return b.policy.SyncUser(ctx, u)
}

// Disable stops the user's running/pending workspaces (freeing their quota
// immediately) before flipping the status gate, per the design spec.
// Status doesn't affect the Casbin grouping policy (disabled users are
// rejected earlier, by the Auth middleware's status check), so no policy
// sync is needed here.
func (b *UserBiz) Disable(ctx context.Context, id string) error {
	if err := b.workspaces.StopActiveForOwner(ctx, id); err != nil {
		return err
	}
	return b.setStatus(ctx, id, "disabled")
}

func (b *UserBiz) Enable(ctx context.Context, id string) error {
	return b.setStatus(ctx, id, "active")
}

func (b *UserBiz) setStatus(ctx context.Context, id, status string) error {
	u, err := b.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	u.Status = status
	return b.repo.Update(ctx, u)
}

// Delete removes the user's owned workspaces (K8s Pod/Service + DB rows)
// before deleting the user row; team_members cascades via the DB's
// ON DELETE CASCADE.
func (b *UserBiz) Delete(ctx context.Context, id string) error {
	if err := b.workspaces.DeleteAllForOwner(ctx, id); err != nil {
		return err
	}
	if err := b.repo.Delete(ctx, id); err != nil {
		return err
	}
	return b.policy.RemoveUser(ctx, id)
}
