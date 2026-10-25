package biz

import (
	"context"
	"errors"
	"time"
)

type Team struct {
	ID          string
	TenantID    string
	Name        string
	QuotaCPU    int
	QuotaMemory int64
	QuotaGPU    int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type TeamRepo interface {
	Create(ctx context.Context, t *Team) error
	Get(ctx context.Context, id string) (*Team, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*Team, error)
	ListByMember(ctx context.Context, userID string) ([]*Team, error)
	Update(ctx context.Context, t *Team) error
	Delete(ctx context.Context, id string) error
	HasWorkspaces(ctx context.Context, teamID string) (bool, error)
	AddMember(ctx context.Context, teamID, userID string) error
	RemoveMember(ctx context.Context, teamID, userID string) error
	IsMember(ctx context.Context, teamID, userID string) (bool, error)
	ListMemberIDs(ctx context.Context, teamID string) ([]string, error)
	// SumActiveWorkspaceRequest sums cpu/memory/gpu for the team's workspaces
	// with status IN (pending, running). Used for the own-commitments quota
	// check. Phase 1: no workspaces exist yet, so this always returns zero.
	SumActiveWorkspaceRequest(ctx context.Context, teamID string) (cpu int, memory int64, gpu int, err error)
}

var (
	ErrTeamQuotaExceedsTenant    = errors.New("team quota would exceed the tenant's remaining quota")
	ErrTeamQuotaBelowCommitments = errors.New("team quota cannot be lower than its own active workspace requests")
	ErrTeamHasWorkspaces         = errors.New("team has active workspaces; delete or reassign them first")
	ErrNotTeamMember             = errors.New("caller is not a member of this team")
)

type TeamBiz struct {
	teams   TeamRepo
	tenants TenantRepo
}

func NewTeamBiz(teams TeamRepo, tenants TenantRepo) *TeamBiz {
	return &TeamBiz{teams: teams, tenants: tenants}
}

func (b *TeamBiz) Create(ctx context.Context, t *Team) error {
	if err := b.checkTenantRemaining(ctx, t.TenantID, t.QuotaCPU, t.QuotaMemory, t.QuotaGPU, ""); err != nil {
		return err
	}
	return b.teams.Create(ctx, t)
}

func (b *TeamBiz) ListByTenant(ctx context.Context, tenantID string) ([]*Team, error) {
	return b.teams.ListByTenant(ctx, tenantID)
}

func (b *TeamBiz) ListByMember(ctx context.Context, userID string) ([]*Team, error) {
	return b.teams.ListByMember(ctx, userID)
}

func (b *TeamBiz) Get(ctx context.Context, id string) (*Team, error) {
	return b.teams.Get(ctx, id)
}

// GetForMember additionally checks that userID actually belongs to the
// team — Casbin's TeamReadAccess only scopes by tenant domain, not team
// membership, so a member viewing "their own team" needs this extra check.
func (b *TeamBiz) GetForMember(ctx context.Context, id, userID string) (*Team, error) {
	isMember, err := b.teams.IsMember(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, ErrNotTeamMember
	}
	return b.teams.Get(ctx, id)
}

func (b *TeamBiz) UpdateQuota(ctx context.Context, id string, cpu int, memory int64, gpu int) (*Team, error) {
	t, err := b.teams.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := b.checkTenantRemaining(ctx, t.TenantID, cpu, memory, gpu, id); err != nil {
		return nil, err
	}
	activeCPU, activeMemory, activeGPU, err := b.teams.SumActiveWorkspaceRequest(ctx, id)
	if err != nil {
		return nil, err
	}
	if cpu < activeCPU || memory < activeMemory || gpu < activeGPU {
		return nil, ErrTeamQuotaBelowCommitments
	}
	t.QuotaCPU, t.QuotaMemory, t.QuotaGPU = cpu, memory, gpu
	if err := b.teams.Update(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (b *TeamBiz) Delete(ctx context.Context, id string) error {
	hasWorkspaces, err := b.teams.HasWorkspaces(ctx, id)
	if err != nil {
		return err
	}
	if hasWorkspaces {
		return ErrTeamHasWorkspaces
	}
	return b.teams.Delete(ctx, id)
}

func (b *TeamBiz) AddMember(ctx context.Context, teamID, userID string) error {
	return b.teams.AddMember(ctx, teamID, userID)
}

func (b *TeamBiz) RemoveMember(ctx context.Context, teamID, userID string) error {
	return b.teams.RemoveMember(ctx, teamID, userID)
}

func (b *TeamBiz) ListMemberIDs(ctx context.Context, teamID string) ([]string, error) {
	return b.teams.ListMemberIDs(ctx, teamID)
}

// checkTenantRemaining ensures sum(all other teams' quota) + the candidate
// quota <= the tenant's quota. excludeTeamID is the team being updated (""
// when creating a new one).
func (b *TeamBiz) checkTenantRemaining(ctx context.Context, tenantID string, cpu int, memory int64, gpu int, excludeTeamID string) error {
	tenant, err := b.tenants.Get(ctx, tenantID)
	if err != nil {
		return err
	}
	others, err := b.teams.ListByTenant(ctx, tenantID)
	if err != nil {
		return err
	}
	var sumCPU, sumGPU int
	var sumMemory int64
	for _, other := range others {
		if other.ID == excludeTeamID {
			continue
		}
		sumCPU += other.QuotaCPU
		sumMemory += other.QuotaMemory
		sumGPU += other.QuotaGPU
	}
	if sumCPU+cpu > tenant.QuotaCPU || sumMemory+memory > tenant.QuotaMemory || sumGPU+gpu > tenant.QuotaGPU {
		return ErrTeamQuotaExceedsTenant
	}
	return nil
}
