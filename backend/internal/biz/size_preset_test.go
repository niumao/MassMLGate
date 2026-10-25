package biz

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// fakeSizePresetRepo is a minimal in-memory stand-in for SizePresetRepo,
// keyed by id — same role as a hand-rolled mock, no external test library
// since none exists in this codebase yet.
type fakeSizePresetRepo struct {
	presets   map[string]*SizePreset
	usedCount map[string]int // keyed by preset name
	nextID    int
}

func newFakeSizePresetRepo() *fakeSizePresetRepo {
	return &fakeSizePresetRepo{presets: map[string]*SizePreset{}, usedCount: map[string]int{}}
}

func (f *fakeSizePresetRepo) Create(ctx context.Context, p *SizePreset) error {
	f.nextID++
	p.ID = fmt.Sprintf("generated-%d", f.nextID)
	f.presets[p.ID] = p
	return nil
}

func (f *fakeSizePresetRepo) Get(ctx context.Context, tenantID, name string) (*SizePreset, error) {
	for _, p := range f.presets {
		if p.TenantID == tenantID && p.Name == name {
			return p, nil
		}
	}
	return nil, ErrSizePresetNotFound
}

func (f *fakeSizePresetRepo) GetByID(ctx context.Context, tenantID, id string) (*SizePreset, error) {
	p, ok := f.presets[id]
	if !ok || p.TenantID != tenantID {
		return nil, ErrSizePresetNotFound
	}
	return p, nil
}

func (f *fakeSizePresetRepo) ListByTenant(ctx context.Context, tenantID string) ([]*SizePreset, error) {
	var out []*SizePreset
	for _, p := range f.presets {
		if p.TenantID == tenantID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeSizePresetRepo) Update(ctx context.Context, p *SizePreset) error {
	f.presets[p.ID] = p
	return nil
}

func (f *fakeSizePresetRepo) Delete(ctx context.Context, tenantID, id string) error {
	delete(f.presets, id)
	return nil
}

func (f *fakeSizePresetRepo) CountWorkspacesUsing(ctx context.Context, tenantID, name string) (int, error) {
	return f.usedCount[name], nil
}

func TestSizePresetBiz_Update_SameNameValueEditAllowedEvenWhileInUse(t *testing.T) {
	repo := newFakeSizePresetRepo()
	p := &SizePreset{TenantID: "t1", Name: "small", CPURequest: 500, MemoryRequest: 1, GPURequest: 0, Storage: 1}
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("setup: %v", err)
	}
	repo.usedCount["small"] = 3 // simulate 3 workspaces referencing this preset

	b := NewSizePresetBiz(repo)
	updated, err := b.Update(context.Background(), "t1", p.ID, "small", 1000, p.MemoryRequest, p.GPURequest, p.Storage)
	if err != nil {
		t.Fatalf("same-name value edit should be allowed while in use, got error: %v", err)
	}
	if updated.CPURequest != 1000 {
		t.Fatalf("CPURequest = %d, want 1000", updated.CPURequest)
	}
}

func TestSizePresetBiz_Update_RenameBlockedWhileInUse(t *testing.T) {
	repo := newFakeSizePresetRepo()
	p := &SizePreset{TenantID: "t1", Name: "small"}
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("setup: %v", err)
	}
	repo.usedCount["small"] = 1

	b := NewSizePresetBiz(repo)
	_, err := b.Update(context.Background(), "t1", p.ID, "tiny", p.CPURequest, p.MemoryRequest, p.GPURequest, p.Storage)
	if !errors.Is(err, ErrSizePresetInUse) {
		t.Fatalf("err = %v, want ErrSizePresetInUse", err)
	}
}

func TestSizePresetBiz_Update_RenameAllowedWhenNotInUse(t *testing.T) {
	repo := newFakeSizePresetRepo()
	p := &SizePreset{TenantID: "t1", Name: "small"}
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// usedCount["small"] defaults to 0 — not in use

	b := NewSizePresetBiz(repo)
	updated, err := b.Update(context.Background(), "t1", p.ID, "tiny", p.CPURequest, p.MemoryRequest, p.GPURequest, p.Storage)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != "tiny" {
		t.Fatalf("Name = %q, want tiny", updated.Name)
	}
}

func TestSizePresetBiz_Delete_BlockedWhileInUse(t *testing.T) {
	repo := newFakeSizePresetRepo()
	p := &SizePreset{TenantID: "t1", Name: "small"}
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("setup: %v", err)
	}
	repo.usedCount["small"] = 2

	b := NewSizePresetBiz(repo)
	if err := b.Delete(context.Background(), "t1", p.ID); !errors.Is(err, ErrSizePresetInUse) {
		t.Fatalf("err = %v, want ErrSizePresetInUse", err)
	}
}

func TestSizePresetBiz_Delete_AllowedWhenNotInUse(t *testing.T) {
	repo := newFakeSizePresetRepo()
	p := &SizePreset{TenantID: "t1", Name: "small"}
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("setup: %v", err)
	}

	b := NewSizePresetBiz(repo)
	if err := b.Delete(context.Background(), "t1", p.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := repo.GetByID(context.Background(), "t1", p.ID); !errors.Is(err, ErrSizePresetNotFound) {
		t.Fatalf("preset should have been deleted")
	}
}

func TestSizePresetBiz_Update_TenantIsolation(t *testing.T) {
	repo := newFakeSizePresetRepo()
	p := &SizePreset{TenantID: "tenant-a", Name: "small"}
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("setup: %v", err)
	}

	b := NewSizePresetBiz(repo)
	_, err := b.Update(context.Background(), "tenant-b", p.ID, "small", 1, 1, 0, 1)
	if !errors.Is(err, ErrSizePresetNotFound) {
		t.Fatalf("err = %v, want ErrSizePresetNotFound (tenant-b must not be able to reach tenant-a's preset)", err)
	}
}
