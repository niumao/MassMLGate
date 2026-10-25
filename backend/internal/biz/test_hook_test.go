package biz

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// fakeTestHookRepo is a minimal in-memory stand-in for TestHookRepo, same
// role as fakeSizePresetRepo/fakeWorkspaceRepo elsewhere in this package.
type fakeTestHookRepo struct {
	hooks  map[string]*TestHook
	nextID int
}

func newFakeTestHookRepo() *fakeTestHookRepo { return &fakeTestHookRepo{hooks: map[string]*TestHook{}} }

func (f *fakeTestHookRepo) Create(ctx context.Context, h *TestHook) error {
	f.nextID++
	h.ID = fmt.Sprintf("hook-%d", f.nextID)
	f.hooks[h.ID] = h
	return nil
}

func (f *fakeTestHookRepo) GetByID(ctx context.Context, tenantID, id string) (*TestHook, error) {
	h, ok := f.hooks[id]
	if !ok || h.TenantID != tenantID {
		return nil, ErrTestHookNotFound
	}
	return h, nil
}

func (f *fakeTestHookRepo) GetByTokenHash(ctx context.Context, tokenHash string) (*TestHook, error) {
	for _, h := range f.hooks {
		if h.TokenHash == tokenHash {
			return h, nil
		}
	}
	return nil, ErrTestHookNotFound
}

func (f *fakeTestHookRepo) ListByWorkspace(ctx context.Context, workspaceID string) ([]*TestHook, error) {
	var out []*TestHook
	for _, h := range f.hooks {
		if h.WorkspaceID == workspaceID {
			out = append(out, h)
		}
	}
	return out, nil
}

func (f *fakeTestHookRepo) Revoke(ctx context.Context, tenantID, id string) error {
	h, ok := f.hooks[id]
	if !ok || h.TenantID != tenantID {
		return ErrTestHookNotFound
	}
	now := time.Now()
	h.RevokedAt = &now
	return nil
}

func (f *fakeTestHookRepo) Touch(ctx context.Context, id string, when time.Time) error {
	if h, ok := f.hooks[id]; ok {
		h.LastTriggeredAt = &when
	}
	return nil
}

type fakeTestRunRepo struct {
	runs   map[string]*TestRun
	nextID int
}

func newFakeTestRunRepo() *fakeTestRunRepo { return &fakeTestRunRepo{runs: map[string]*TestRun{}} }

func (f *fakeTestRunRepo) Create(ctx context.Context, r *TestRun) error {
	f.nextID++
	r.ID = fmt.Sprintf("run-%d", f.nextID)
	f.runs[r.ID] = r
	return nil
}

func (f *fakeTestRunRepo) Update(ctx context.Context, r *TestRun) error {
	f.runs[r.ID] = r
	return nil
}

func (f *fakeTestRunRepo) Get(ctx context.Context, id string) (*TestRun, error) {
	r, ok := f.runs[id]
	if !ok {
		return nil, ErrTestRunNotFound
	}
	return r, nil
}

func (f *fakeTestRunRepo) ListByHook(ctx context.Context, hookID string) ([]*TestRun, error) {
	var out []*TestRun
	for _, r := range f.runs {
		if r.TestHookID == hookID {
			out = append(out, r)
		}
	}
	return out, nil
}

// fakeTestJobProvisioner invokes onStatus synchronously (from within the
// goroutine TestHookBiz.Trigger launches) and signals completion on done so
// tests can deterministically wait for the async callback instead of
// sleeping/polling.
type fakeTestJobProvisioner struct {
	status TestRunStatus
	errMsg string
	done   chan struct{}
}

func newFakeTestJobProvisioner(status TestRunStatus) *fakeTestJobProvisioner {
	return &fakeTestJobProvisioner{status: status, done: make(chan struct{}, 1)}
}

func (f *fakeTestJobProvisioner) Run(_ context.Context, _ string, _ *TestHook, _ *TestRun, onStatus func(status TestRunStatus, errMsg, jobName, podName string)) {
	onStatus(f.status, f.errMsg, "job-x", "pod-x")
	f.done <- struct{}{}
}

func (f *fakeTestJobProvisioner) FetchLogs(_ context.Context, _, _ string, _ int64) (string, error) {
	return "fake logs", nil
}

func newTestHookFixture(status TestRunStatus) (*TestHookBiz, *fakeWorkspaceRepo, *fakeTestHookRepo, *fakeTestRunRepo, *fakeTestJobProvisioner) {
	wsRepo := newFakeWorkspaceRepo()
	wsRepo.workspaces["ws-1"] = &Workspace{ID: "ws-1", TenantID: "t1", OwnerUserID: "owner-1"}
	tenants := &fakeTenantRepoForWorkspaceTest{tenant: &Tenant{ID: "t1", Namespace: "ns-1"}}
	hookRepo := newFakeTestHookRepo()
	runRepo := newFakeTestRunRepo()
	provisioner := newFakeTestJobProvisioner(status)
	b := NewTestHookBiz(hookRepo, runRepo, wsRepo, tenants, provisioner)
	return b, wsRepo, hookRepo, runRepo, provisioner
}

func TestTestHookBiz_Create_RequiresOwner(t *testing.T) {
	b, _, _, _, _ := newTestHookFixture(TestRunPassed)

	_, _, err := b.Create(context.Background(), "ws-1", "not-the-owner", "ci", "myimg", nil, "")
	if !errors.Is(err, ErrNotWorkspaceOwner) {
		t.Fatalf("err = %v, want ErrNotWorkspaceOwner", err)
	}
}

func TestTestHookBiz_Create_TokenHashRoundTrip(t *testing.T) {
	b, _, _, _, _ := newTestHookFixture(TestRunPassed)

	hook, token, err := b.Create(context.Background(), "ws-1", "owner-1", "ci", "myimg", []string{"pytest"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token == "" {
		t.Fatalf("expected a raw token to be returned")
	}
	if hook.TokenHash == token {
		t.Fatalf("stored hash must not equal the raw token")
	}
	if hook.TokenHash != hashToken(token) {
		t.Fatalf("stored hash does not match the hash of the returned token")
	}
}

func TestTestHookBiz_Trigger_UnknownTokenNotFound(t *testing.T) {
	b, _, _, _, _ := newTestHookFixture(TestRunPassed)

	if _, err := b.Trigger(context.Background(), "nonexistent-token", "", ""); !errors.Is(err, ErrTestHookNotFound) {
		t.Fatalf("err = %v, want ErrTestHookNotFound", err)
	}
}

// Revoked and unknown tokens must be indistinguishable to the caller — see
// TestHookBiz.Trigger's comment on why (a leaked/guessed token must not be
// usable to probe revocation status).
func TestTestHookBiz_Trigger_RevokedTokenMapsToSameNotFoundError(t *testing.T) {
	b, _, _, _, _ := newTestHookFixture(TestRunPassed)

	hook, token, err := b.Create(context.Background(), "ws-1", "owner-1", "ci", "myimg", nil, "")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := b.Revoke(context.Background(), "ws-1", hook.ID, "owner-1"); err != nil {
		t.Fatalf("setup revoke: %v", err)
	}

	if _, err := b.Trigger(context.Background(), token, "", ""); !errors.Is(err, ErrTestHookNotFound) {
		t.Fatalf("err = %v, want ErrTestHookNotFound", err)
	}
}

func TestTestHookBiz_Trigger_SuccessTransitionsPendingToPassed(t *testing.T) {
	b, _, _, runRepo, provisioner := newTestHookFixture(TestRunPassed)

	_, token, err := b.Create(context.Background(), "ws-1", "owner-1", "ci", "myimg", nil, "")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	run, err := b.Trigger(context.Background(), token, "deadbeef", "v2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run.Status != TestRunPending {
		t.Fatalf("Status immediately after Trigger = %v, want pending", run.Status)
	}

	select {
	case <-provisioner.done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the async provisioner callback")
	}

	updated, err := runRepo.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Status != TestRunPassed {
		t.Fatalf("Status after callback = %v, want passed", updated.Status)
	}
	if updated.FinishedAt == nil {
		t.Fatalf("FinishedAt should be set once the run reaches a terminal status")
	}
}

func TestTestHookBiz_Trigger_FailureTransitionsPendingToFailed(t *testing.T) {
	b, _, _, runRepo, provisioner := newTestHookFixture(TestRunFailed)

	_, token, err := b.Create(context.Background(), "ws-1", "owner-1", "ci", "myimg", nil, "")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	run, err := b.Trigger(context.Background(), token, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-provisioner.done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the async provisioner callback")
	}

	updated, err := runRepo.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Status != TestRunFailed {
		t.Fatalf("Status after callback = %v, want failed", updated.Status)
	}
}

func TestTestHookBiz_Revoke_RequiresOwner(t *testing.T) {
	b, _, _, _, _ := newTestHookFixture(TestRunPassed)

	hook, _, err := b.Create(context.Background(), "ws-1", "owner-1", "ci", "myimg", nil, "")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := b.Revoke(context.Background(), "ws-1", hook.ID, "someone-else"); !errors.Is(err, ErrNotWorkspaceOwner) {
		t.Fatalf("err = %v, want ErrNotWorkspaceOwner", err)
	}
}

func TestTestHookBiz_ListByWorkspace_TenantIsolation(t *testing.T) {
	b, _, _, _, _ := newTestHookFixture(TestRunPassed)

	if _, _, err := b.Create(context.Background(), "ws-1", "owner-1", "ci", "myimg", nil, ""); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if _, err := b.ListByWorkspace(context.Background(), "ws-1", "some-other-tenant"); !errors.Is(err, ErrTestHookNotFound) {
		t.Fatalf("err = %v, want ErrTestHookNotFound (a different tenant must not see this workspace's hooks)", err)
	}

	list, err := b.ListByWorkspace(context.Background(), "ws-1", "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
}
