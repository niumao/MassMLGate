package biz

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// fakeTokenQuotaRepo is a minimal in-memory stand-in for TokenQuotaRepo,
// keyed by id — same approach as fakeSizePresetRepo (size_preset_test.go).
type fakeTokenQuotaRepo struct {
	quotas map[string]*TokenQuota
	nextID int
}

func newFakeTokenQuotaRepo() *fakeTokenQuotaRepo {
	return &fakeTokenQuotaRepo{quotas: map[string]*TokenQuota{}}
}

func (f *fakeTokenQuotaRepo) Create(ctx context.Context, q *TokenQuota) error {
	f.nextID++
	q.ID = fmt.Sprintf("generated-%d", f.nextID)
	f.quotas[q.ID] = q
	return nil
}

func (f *fakeTokenQuotaRepo) GetByID(ctx context.Context, tenantID, id string) (*TokenQuota, error) {
	q, ok := f.quotas[id]
	if !ok || q.TenantID != tenantID {
		return nil, ErrTokenQuotaNotFound
	}
	return q, nil
}

func (f *fakeTokenQuotaRepo) ListByTenant(ctx context.Context, tenantID string) ([]*TokenQuota, error) {
	var out []*TokenQuota
	for _, q := range f.quotas {
		if q.TenantID == tenantID {
			out = append(out, q)
		}
	}
	return out, nil
}

func (f *fakeTokenQuotaRepo) Update(ctx context.Context, q *TokenQuota) error {
	f.quotas[q.ID] = q
	return nil
}

func (f *fakeTokenQuotaRepo) Delete(ctx context.Context, tenantID, id string) error {
	delete(f.quotas, id)
	return nil
}

// fakeTenantRepoForQuota implements just enough of TenantRepo for
// TokenQuotaBiz.namespaceFor — the other methods are never called by it.
type fakeTenantRepoForQuota struct {
	namespace string
}

func (f *fakeTenantRepoForQuota) Create(ctx context.Context, t *Tenant) error { return nil }
func (f *fakeTenantRepoForQuota) Get(ctx context.Context, id string) (*Tenant, error) {
	return &Tenant{ID: id, Namespace: f.namespace}, nil
}
func (f *fakeTenantRepoForQuota) List(ctx context.Context) ([]*Tenant, error) { return nil, nil }
func (f *fakeTenantRepoForQuota) Update(ctx context.Context, t *Tenant) error { return nil }
func (f *fakeTenantRepoForQuota) Delete(ctx context.Context, id string) error { return nil }
func (f *fakeTenantRepoForQuota) SumTeamQuota(ctx context.Context, tenantID string) (int, int64, int, error) {
	return 0, 0, 0, nil
}

// fakeTokenQuotaProvisioner records what was reconciled/torn down, so tests
// can assert TokenQuotaBiz actually calls into the gateway provisioner and
// not just Postgres.
type fakeTokenQuotaProvisioner struct {
	ensured []string // "ns/id" per EnsureQuota call
	deleted []string
}

func (f *fakeTokenQuotaProvisioner) EnsureQuota(ctx context.Context, ns string, q *TokenQuota) error {
	f.ensured = append(f.ensured, ns+"/"+q.ID)
	return nil
}

func (f *fakeTokenQuotaProvisioner) DeleteQuota(ctx context.Context, ns string, q *TokenQuota) error {
	f.deleted = append(f.deleted, ns+"/"+q.ID)
	return nil
}

func TestTokenQuotaBiz_Create_ReconcilesProvisioner(t *testing.T) {
	repo := newFakeTokenQuotaRepo()
	tenants := &fakeTenantRepoForQuota{namespace: "acme"}
	prov := &fakeTokenQuotaProvisioner{}
	b := NewTokenQuotaBiz(repo, tenants, prov)

	q := &TokenQuota{TenantID: "t1", ModelName: "gpt-4", TokenLimit: 1_000_000, Window: "daily"}
	if err := b.Create(context.Background(), q); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prov.ensured) != 1 || prov.ensured[0] != "acme/"+q.ID {
		t.Fatalf("ensured = %v, want exactly one call for acme/%s", prov.ensured, q.ID)
	}
}

func TestTokenQuotaBiz_Update_ReReconcilesWithNewLimit(t *testing.T) {
	repo := newFakeTokenQuotaRepo()
	tenants := &fakeTenantRepoForQuota{namespace: "acme"}
	prov := &fakeTokenQuotaProvisioner{}
	b := NewTokenQuotaBiz(repo, tenants, prov)

	q := &TokenQuota{TenantID: "t1", ModelName: "gpt-4", TokenLimit: 1_000_000, Window: "daily"}
	if err := b.Create(context.Background(), q); err != nil {
		t.Fatalf("setup: %v", err)
	}

	updated, err := b.Update(context.Background(), "t1", q.ID, "gpt-4", 2_000_000, "monthly")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.TokenLimit != 2_000_000 || updated.Window != "monthly" {
		t.Fatalf("updated = %+v, want TokenLimit=2000000 Window=monthly", updated)
	}
	if len(prov.ensured) != 2 {
		t.Fatalf("ensured calls = %d, want 2 (Create + Update)", len(prov.ensured))
	}
}

func TestTokenQuotaBiz_Delete_TearsDownProvisionerThenRepo(t *testing.T) {
	repo := newFakeTokenQuotaRepo()
	tenants := &fakeTenantRepoForQuota{namespace: "acme"}
	prov := &fakeTokenQuotaProvisioner{}
	b := NewTokenQuotaBiz(repo, tenants, prov)

	q := &TokenQuota{TenantID: "t1", ModelName: "gpt-4", TokenLimit: 1_000_000, Window: "daily"}
	if err := b.Create(context.Background(), q); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := b.Delete(context.Background(), "t1", q.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prov.deleted) != 1 || prov.deleted[0] != "acme/"+q.ID {
		t.Fatalf("deleted = %v, want exactly one call for acme/%s", prov.deleted, q.ID)
	}
	if _, err := repo.GetByID(context.Background(), "t1", q.ID); !errors.Is(err, ErrTokenQuotaNotFound) {
		t.Fatalf("quota should have been deleted from the repo")
	}
}

func TestTokenQuotaBiz_Update_TenantIsolation(t *testing.T) {
	repo := newFakeTokenQuotaRepo()
	tenants := &fakeTenantRepoForQuota{namespace: "acme"}
	prov := &fakeTokenQuotaProvisioner{}
	b := NewTokenQuotaBiz(repo, tenants, prov)

	q := &TokenQuota{TenantID: "tenant-a", ModelName: "gpt-4", TokenLimit: 1_000_000, Window: "daily"}
	if err := b.Create(context.Background(), q); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err := b.Update(context.Background(), "tenant-b", q.ID, "gpt-4", 1, "daily")
	if !errors.Is(err, ErrTokenQuotaNotFound) {
		t.Fatalf("err = %v, want ErrTokenQuotaNotFound (tenant-b must not reach tenant-a's quota)", err)
	}
}
