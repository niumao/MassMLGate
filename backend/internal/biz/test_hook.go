package biz

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

type TestRunStatus string

const (
	TestRunPending TestRunStatus = "pending"
	TestRunRunning TestRunStatus = "running"
	TestRunPassed  TestRunStatus = "passed"
	TestRunFailed  TestRunStatus = "failed"
)

// TestHook is a per-workspace, owner-configured trigger: a tenant's own
// external CI (GitHub Actions/GitLab CI) POSTs the hook's secret token after
// a code/model update, and the platform runs the fixed TestImage/TestCommand
// as a one-shot K8s Job in the workspace's tenant namespace. TestImage and
// TestCommand are configured once here, not supplied per-trigger — the
// trigger payload (commit/image tag) is recorded as metadata only, never
// used to pick what gets run. That's what keeps this a test runner rather
// than a model registry: this service never pulls/promotes an arbitrary
// tenant-supplied artifact, it always runs the one image the owner
// pre-approved.
type TestHook struct {
	ID              string
	TenantID        string
	WorkspaceID     string
	Name            string
	TokenHash       string // sha256 hex digest; the raw token is never stored
	TestImage       string
	TestCommand     []string
	ImagePullSecret string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	RevokedAt       *time.Time
	LastTriggeredAt *time.Time
}

type TestRun struct {
	ID           string
	TestHookID   string
	WorkspaceID  string
	TenantID     string
	CommitSHA    string
	ImageTag     string
	Status       TestRunStatus
	K8sJobName   string
	K8sPodName   string
	ErrorMessage string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	FinishedAt   *time.Time
}

type TestHookRepo interface {
	Create(ctx context.Context, h *TestHook) error
	GetByID(ctx context.Context, tenantID, id string) (*TestHook, error)
	// GetByTokenHash is the only lookup the anonymous trigger route uses —
	// deliberately not tenant-scoped, since the external caller supplies no
	// tenant/JWT context at all, only the token itself.
	GetByTokenHash(ctx context.Context, tokenHash string) (*TestHook, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]*TestHook, error)
	Revoke(ctx context.Context, tenantID, id string) error
	Touch(ctx context.Context, id string, when time.Time) error
}

type TestRunRepo interface {
	Create(ctx context.Context, r *TestRun) error
	Update(ctx context.Context, r *TestRun) error
	Get(ctx context.Context, id string) (*TestRun, error)
	ListByHook(ctx context.Context, hookID string) ([]*TestRun, error)
}

// TestJobProvisioner runs a single batchv1.Job per trigger — one-shot, no
// Start/Stop/Delete, since Kubernetes' own ttlSecondsAfterFinished garbage
// collects it. FetchLogs is best-effort and only works until that GC fires.
type TestJobProvisioner interface {
	Run(ctx context.Context, tenantNamespace string, hook *TestHook, run *TestRun,
		onStatus func(status TestRunStatus, errMsg, jobName, podName string))
	FetchLogs(ctx context.Context, tenantNamespace, podName string, tailLines int64) (string, error)
}

var (
	ErrTestHookNotFound = errors.New("test hook not found")
	ErrTestHookRevoked  = errors.New("test hook has been revoked")
	ErrTestRunNotFound  = errors.New("test run not found")
)

type TestHookBiz struct {
	hooks       TestHookRepo
	runs        TestRunRepo
	workspaces  WorkspaceRepo
	tenants     TenantRepo
	provisioner TestJobProvisioner
}

func NewTestHookBiz(hooks TestHookRepo, runs TestRunRepo, workspaces WorkspaceRepo, tenants TenantRepo, provisioner TestJobProvisioner) *TestHookBiz {
	return &TestHookBiz{hooks: hooks, runs: runs, workspaces: workspaces, tenants: tenants, provisioner: provisioner}
}

func (b *TestHookBiz) namespaceFor(ctx context.Context, tenantID string) (string, error) {
	t, err := b.tenants.Get(ctx, tenantID)
	if err != nil {
		return "", err
	}
	return t.Namespace, nil
}

func newToken() (raw, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(raw))
	hash = hex.EncodeToString(sum[:])
	return raw, hash, nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Create requires callerUserID to own workspaceID (same ownership rule as
// WorkspaceBiz — see ErrNotWorkspaceOwner). The raw token is returned
// exactly once; only its hash is ever persisted, so it can't be recovered
// later — same UX as a GitHub PAT or a Render/Vercel deploy hook.
func (b *TestHookBiz) Create(ctx context.Context, workspaceID, callerUserID, name, testImage string, testCommand []string, imagePullSecret string) (*TestHook, string, error) {
	w, err := b.workspaces.Get(ctx, workspaceID)
	if err != nil {
		return nil, "", err
	}
	if w.OwnerUserID != callerUserID {
		return nil, "", ErrNotWorkspaceOwner
	}
	raw, hash, err := newToken()
	if err != nil {
		return nil, "", err
	}
	h := &TestHook{
		TenantID: w.TenantID, WorkspaceID: workspaceID, Name: name,
		TokenHash: hash, TestImage: testImage, TestCommand: testCommand, ImagePullSecret: imagePullSecret,
	}
	if err := b.hooks.Create(ctx, h); err != nil {
		return nil, "", err
	}
	return h, raw, nil
}

// ListByWorkspace is shared by the owning member and tenant_admin's
// tenant-wide read-only drill-down — tenant-checked, not owner-checked, to
// match WorkspaceReadAccess's reach.
func (b *TestHookBiz) ListByWorkspace(ctx context.Context, workspaceID, callerTenantID string) ([]*TestHook, error) {
	w, err := b.workspaces.Get(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if w.TenantID != callerTenantID {
		return nil, ErrTestHookNotFound
	}
	return b.hooks.ListByWorkspace(ctx, workspaceID)
}

func (b *TestHookBiz) Revoke(ctx context.Context, workspaceID, hookID, callerUserID string) error {
	w, err := b.workspaces.Get(ctx, workspaceID)
	if err != nil {
		return err
	}
	if w.OwnerUserID != callerUserID {
		return ErrNotWorkspaceOwner
	}
	h, err := b.hooks.GetByID(ctx, w.TenantID, hookID)
	if err != nil {
		return err
	}
	if h.WorkspaceID != workspaceID {
		return ErrTestHookNotFound
	}
	return b.hooks.Revoke(ctx, w.TenantID, hookID)
}

func (b *TestHookBiz) ListRuns(ctx context.Context, hookID, callerTenantID string) ([]*TestRun, error) {
	h, err := b.hooks.GetByID(ctx, callerTenantID, hookID)
	if err != nil {
		return nil, err
	}
	return b.runs.ListByHook(ctx, h.ID)
}

func (b *TestHookBiz) RunLogs(ctx context.Context, runID, callerTenantID string) (string, error) {
	run, err := b.runs.Get(ctx, runID)
	if err != nil {
		return "", err
	}
	if run.TenantID != callerTenantID {
		return "", ErrTestRunNotFound
	}
	ns, err := b.namespaceFor(ctx, run.TenantID)
	if err != nil {
		return "", err
	}
	return b.provisioner.FetchLogs(ctx, ns, run.K8sPodName, 200)
}

// Trigger is the anonymous webhook entrypoint — rawToken is whatever the
// caller's CI POSTed, with no other auth. Both "no such token" and "revoked"
// map to the same ErrTestHookNotFound here; the handler must keep mapping
// both to the same generic 404, never letting the response distinguish
// them, or a leaked/guessed token could be used to probe revocation status.
func (b *TestHookBiz) Trigger(ctx context.Context, rawToken, commitSHA, imageTag string) (*TestRun, error) {
	hook, err := b.hooks.GetByTokenHash(ctx, hashToken(rawToken))
	if err != nil {
		return nil, err
	}
	if hook.RevokedAt != nil {
		return nil, ErrTestHookNotFound
	}
	run := &TestRun{
		TestHookID: hook.ID, WorkspaceID: hook.WorkspaceID, TenantID: hook.TenantID,
		CommitSHA: commitSHA, ImageTag: imageTag, Status: TestRunPending,
	}
	if err := b.runs.Create(ctx, run); err != nil {
		return nil, err
	}
	_ = b.hooks.Touch(ctx, hook.ID, time.Now())

	ns, err := b.namespaceFor(ctx, hook.TenantID)
	if err != nil {
		return nil, err
	}
	go b.provisioner.Run(context.Background(), ns, hook, run, b.statusCallback(run))
	return run, nil
}

// statusCallback persists whatever the provisioner reports back to
// Postgres — same role as WorkspaceBiz.statusCallback.
func (b *TestHookBiz) statusCallback(run *TestRun) func(status TestRunStatus, errMsg, jobName, podName string) {
	return func(status TestRunStatus, errMsg, jobName, podName string) {
		run.Status = status
		run.ErrorMessage = errMsg
		run.K8sJobName = jobName
		run.K8sPodName = podName
		if status == TestRunPassed || status == TestRunFailed {
			now := time.Now()
			run.FinishedAt = &now
		}
		_ = b.runs.Update(context.Background(), run)
	}
}
