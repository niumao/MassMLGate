package biz

import (
	"context"
	"errors"
	"testing"
)

type fakeWorkspaceRepo struct {
	workspaces map[string]*Workspace
}

func newFakeWorkspaceRepo() *fakeWorkspaceRepo {
	return &fakeWorkspaceRepo{workspaces: map[string]*Workspace{}}
}

func (f *fakeWorkspaceRepo) Create(ctx context.Context, w *Workspace) error {
	w.ID = "ws-generated"
	f.workspaces[w.ID] = w
	return nil
}
func (f *fakeWorkspaceRepo) Get(ctx context.Context, id string) (*Workspace, error) {
	w, ok := f.workspaces[id]
	if !ok {
		return nil, errors.New("workspace not found")
	}
	return w, nil
}
func (f *fakeWorkspaceRepo) ListByOwner(ctx context.Context, ownerUserID string) ([]*Workspace, error) {
	return nil, nil
}
func (f *fakeWorkspaceRepo) ListByTenant(ctx context.Context, tenantID string) ([]*Workspace, error) {
	return nil, nil
}
func (f *fakeWorkspaceRepo) ListActiveByOwner(ctx context.Context, ownerUserID string) ([]*Workspace, error) {
	return nil, nil
}
func (f *fakeWorkspaceRepo) Update(ctx context.Context, w *Workspace) error {
	f.workspaces[w.ID] = w
	return nil
}
func (f *fakeWorkspaceRepo) Delete(ctx context.Context, id string) error {
	delete(f.workspaces, id)
	return nil
}

type fakeTeamRepoForWorkspaceTest struct{ team *Team }

func (f *fakeTeamRepoForWorkspaceTest) Create(ctx context.Context, t *Team) error { return nil }
func (f *fakeTeamRepoForWorkspaceTest) Get(ctx context.Context, id string) (*Team, error) {
	return f.team, nil
}
func (f *fakeTeamRepoForWorkspaceTest) ListByTenant(ctx context.Context, tenantID string) ([]*Team, error) {
	return nil, nil
}
func (f *fakeTeamRepoForWorkspaceTest) ListByMember(ctx context.Context, userID string) ([]*Team, error) {
	return nil, nil
}
func (f *fakeTeamRepoForWorkspaceTest) Update(ctx context.Context, t *Team) error   { return nil }
func (f *fakeTeamRepoForWorkspaceTest) Delete(ctx context.Context, id string) error { return nil }
func (f *fakeTeamRepoForWorkspaceTest) HasWorkspaces(ctx context.Context, teamID string) (bool, error) {
	return false, nil
}
func (f *fakeTeamRepoForWorkspaceTest) AddMember(ctx context.Context, teamID, userID string) error {
	return nil
}
func (f *fakeTeamRepoForWorkspaceTest) RemoveMember(ctx context.Context, teamID, userID string) error {
	return nil
}
func (f *fakeTeamRepoForWorkspaceTest) IsMember(ctx context.Context, teamID, userID string) (bool, error) {
	return false, nil
}
func (f *fakeTeamRepoForWorkspaceTest) ListMemberIDs(ctx context.Context, teamID string) ([]string, error) {
	return nil, nil
}
func (f *fakeTeamRepoForWorkspaceTest) SumActiveWorkspaceRequest(ctx context.Context, teamID string) (int, int64, int, error) {
	return 0, 0, 0, nil
}

type fakeTenantRepoForWorkspaceTest struct{ tenant *Tenant }

func (f *fakeTenantRepoForWorkspaceTest) Create(ctx context.Context, t *Tenant) error { return nil }
func (f *fakeTenantRepoForWorkspaceTest) Get(ctx context.Context, id string) (*Tenant, error) {
	return f.tenant, nil
}
func (f *fakeTenantRepoForWorkspaceTest) List(ctx context.Context) ([]*Tenant, error) {
	return nil, nil
}
func (f *fakeTenantRepoForWorkspaceTest) Update(ctx context.Context, t *Tenant) error { return nil }
func (f *fakeTenantRepoForWorkspaceTest) Delete(ctx context.Context, id string) error { return nil }
func (f *fakeTenantRepoForWorkspaceTest) SumTeamQuota(ctx context.Context, tenantID string) (int, int64, int, error) {
	return 0, 0, 0, nil
}

type fakeWorkspaceProvisioner struct{}

func (f *fakeWorkspaceProvisioner) Create(ctx context.Context, tenantNamespace string, w *Workspace, onStatus func(WorkspaceStatus, string, string)) {
}
func (f *fakeWorkspaceProvisioner) Start(ctx context.Context, tenantNamespace string, w *Workspace, onStatus func(WorkspaceStatus, string, string)) {
}
func (f *fakeWorkspaceProvisioner) Stop(ctx context.Context, tenantNamespace string, w *Workspace) error {
	return nil
}
func (f *fakeWorkspaceProvisioner) Delete(ctx context.Context, tenantNamespace string, w *Workspace) error {
	return nil
}

func TestWorkspaceBiz_Create_UnknownSizeReturnsErrSizePresetNotFound(t *testing.T) {
	presets := newFakeSizePresetRepo() // empty — tenant-1 has no presets configured
	teamRepo := &fakeTeamRepoForWorkspaceTest{team: &Team{ID: "team-1", TenantID: "tenant-1", QuotaCPU: 10000, QuotaMemory: 10 << 30, QuotaGPU: 4}}
	tenantRepo := &fakeTenantRepoForWorkspaceTest{tenant: &Tenant{ID: "tenant-1", Namespace: "ns-1"}}
	b := NewWorkspaceBiz(newFakeWorkspaceRepo(), teamRepo, tenantRepo, presets, &fakeWorkspaceProvisioner{})

	w := &Workspace{TenantID: "tenant-1", TeamID: "team-1", Size: "small", ImageKey: "jupyter"}
	err := b.Create(context.Background(), w)
	if !errors.Is(err, ErrSizePresetNotFound) {
		t.Fatalf("err = %v, want ErrSizePresetNotFound", err)
	}
}

func TestWorkspaceBiz_Create_PopulatesRequestFieldsFromPreset(t *testing.T) {
	presets := newFakeSizePresetRepo()
	preset := &SizePreset{TenantID: "tenant-1", Name: "small", CPURequest: 500, MemoryRequest: 1 << 30, GPURequest: 2, Storage: 5 << 30}
	if err := presets.Create(context.Background(), preset); err != nil {
		t.Fatalf("setup: %v", err)
	}
	teamRepo := &fakeTeamRepoForWorkspaceTest{team: &Team{ID: "team-1", TenantID: "tenant-1", QuotaCPU: 10000, QuotaMemory: 10 << 30, QuotaGPU: 4}}
	tenantRepo := &fakeTenantRepoForWorkspaceTest{tenant: &Tenant{ID: "tenant-1", Namespace: "ns-1"}}
	b := NewWorkspaceBiz(newFakeWorkspaceRepo(), teamRepo, tenantRepo, presets, &fakeWorkspaceProvisioner{})

	w := &Workspace{TenantID: "tenant-1", TeamID: "team-1", Size: "small", ImageKey: "jupyter"}
	if err := b.Create(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.CPURequest != preset.CPURequest || w.MemoryRequest != preset.MemoryRequest || w.GPURequest != preset.GPURequest || w.StorageRequest != preset.Storage {
		t.Fatalf("w = {CPU:%d Memory:%d GPU:%d Storage:%d}, want {CPU:%d Memory:%d GPU:%d Storage:%d}",
			w.CPURequest, w.MemoryRequest, w.GPURequest, w.StorageRequest, preset.CPURequest, preset.MemoryRequest, preset.GPURequest, preset.Storage)
	}
}

func TestWorkspaceBiz_Start_UnknownSizeReturnsErrSizePresetNotFound(t *testing.T) {
	presets := newFakeSizePresetRepo()
	teamRepo := &fakeTeamRepoForWorkspaceTest{team: &Team{ID: "team-1", TenantID: "tenant-1", QuotaCPU: 10000, QuotaMemory: 10 << 30, QuotaGPU: 4}}
	tenantRepo := &fakeTenantRepoForWorkspaceTest{tenant: &Tenant{ID: "tenant-1", Namespace: "ns-1"}}
	workspaceRepo := newFakeWorkspaceRepo()
	w := &Workspace{ID: "ws-1", TenantID: "tenant-1", TeamID: "team-1", Size: "small", OwnerUserID: "user-1", Status: WorkspaceStopped}
	workspaceRepo.workspaces[w.ID] = w
	b := NewWorkspaceBiz(workspaceRepo, teamRepo, tenantRepo, presets, &fakeWorkspaceProvisioner{})

	err := b.Start(context.Background(), w.ID, "user-1")
	if !errors.Is(err, ErrSizePresetNotFound) {
		t.Fatalf("err = %v, want ErrSizePresetNotFound", err)
	}
}
