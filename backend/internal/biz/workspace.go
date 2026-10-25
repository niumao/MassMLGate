package biz

import (
	"context"
	"errors"
	"time"
)

type WorkspaceStatus string

const (
	WorkspacePending WorkspaceStatus = "pending"
	WorkspaceRunning WorkspaceStatus = "running"
	WorkspaceStopped WorkspaceStatus = "stopped"
	WorkspaceFailed  WorkspaceStatus = "failed"
)

type Workspace struct {
	ID             string
	TenantID       string
	TeamID         string
	OwnerUserID    string
	Name           string
	ImageKey       string
	Size           string
	CPURequest     int
	MemoryRequest  int64
	GPURequest     int
	StorageRequest int64
	Status         WorkspaceStatus
	K8sPodName     string
	K8sServiceName string
	ErrorMessage   string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	StoppedAt      *time.Time
}

var ImageKeys = []string{"jupyter", "vscode"}

// ImagePorts is each image's container port — Jupyter and code-server
// don't share a default port, so this can't be a single constant.
var ImagePorts = map[string]int{"jupyter": 8888, "vscode": 8080}

type WorkspaceRepo interface {
	Create(ctx context.Context, w *Workspace) error
	Get(ctx context.Context, id string) (*Workspace, error)
	ListByOwner(ctx context.Context, ownerUserID string) ([]*Workspace, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*Workspace, error)
	ListActiveByOwner(ctx context.Context, ownerUserID string) ([]*Workspace, error)
	Update(ctx context.Context, w *Workspace) error
	Delete(ctx context.Context, id string) error
}

// WorkspaceProvisioner reconciles a workspace's K8s StatefulSet+Service, in
// the tenant's namespace (identified by tenantNamespace, the tenant's
// `namespace` slug — the same one namespaceName() derives the
// mlops-t-{namespace} K8s namespace from). Implemented by internal/pkg/k8s;
// a no-op implementation is used when K8s isn't available (local dev).
// Create/Start run asynchronously after the initial DB row is written — see
// WorkspaceBiz.Create/Start.
//
// Workspaces run as a single-replica StatefulSet rather than a bare Pod —
// that's what makes Stop/Start a simple replica-count toggle (0 or 1)
// instead of delete-and-recreate, and it's what gives the workspace a
// persistent "save" volume that reattaches on every (re)start, crash or
// not. See internal/pkg/k8s/workspace_provisioner.go for the full reasoning.
type WorkspaceProvisioner interface {
	// Create provisions the StatefulSet+Service for w and polls for the Pod
	// to become Running, updating w's status/error via onStatus as the poll
	// resolves (per the design spec's 5s/2min poll loop).
	Create(ctx context.Context, tenantNamespace string, w *Workspace, onStatus func(status WorkspaceStatus, errMsg string, podName string))
	// Start scales an existing (previously Stopped) StatefulSet back to 1
	// replica and polls for Running again — the saved-data volume from
	// Create reattaches unchanged.
	Start(ctx context.Context, tenantNamespace string, w *Workspace, onStatus func(status WorkspaceStatus, errMsg string, podName string))
	// Stop scales the StatefulSet to 0 replicas, freeing its GPU/CPU/memory
	// immediately. The StatefulSet, Service, and saved-data volume all stay.
	Stop(ctx context.Context, tenantNamespace string, w *Workspace) error
	// Delete removes the StatefulSet and Service; per its retention policy,
	// this also deletes the saved-data volume — permanent, unlike Stop.
	Delete(ctx context.Context, tenantNamespace string, w *Workspace) error
}

var (
	ErrUnknownImage      = errors.New("unknown workspace image")
	ErrTeamQuotaExceeded = errors.New("workspace would exceed the team's remaining quota")
	ErrNotWorkspaceOwner = errors.New("caller does not own this workspace")
)

type WorkspaceBiz struct {
	repo        WorkspaceRepo
	teams       TeamRepo
	tenants     TenantRepo
	presets     SizePresetRepo
	provisioner WorkspaceProvisioner
}

func NewWorkspaceBiz(repo WorkspaceRepo, teams TeamRepo, tenants TenantRepo, presets SizePresetRepo, provisioner WorkspaceProvisioner) *WorkspaceBiz {
	return &WorkspaceBiz{repo: repo, teams: teams, tenants: tenants, presets: presets, provisioner: provisioner}
}

func (b *WorkspaceBiz) namespaceFor(ctx context.Context, tenantID string) (string, error) {
	t, err := b.tenants.Get(ctx, tenantID)
	if err != nil {
		return "", err
	}
	return t.Namespace, nil
}

func (b *WorkspaceBiz) checkQuota(ctx context.Context, teamID string, preset SizePreset) error {
	team, err := b.teams.Get(ctx, teamID)
	if err != nil {
		return err
	}
	activeCPU, activeMemory, activeGPU, err := b.teams.SumActiveWorkspaceRequest(ctx, teamID)
	if err != nil {
		return err
	}
	if activeCPU+preset.CPURequest > team.QuotaCPU ||
		activeMemory+preset.MemoryRequest > team.QuotaMemory ||
		activeGPU+preset.GPURequest > team.QuotaGPU {
		return ErrTeamQuotaExceeded
	}
	return nil
}

// Create validates the image/size catalog and team quota remaining, inserts
// a pending row, then asynchronously provisions the Pod/Service and polls
// for Running — see the design spec's Workspace Provisioning Flow.
func (b *WorkspaceBiz) Create(ctx context.Context, w *Workspace) error {
	if !isKnownImage(w.ImageKey) {
		return ErrUnknownImage
	}
	preset, err := b.presets.Get(ctx, w.TenantID, w.Size)
	if err != nil {
		return err // ErrSizePresetNotFound if w.Size doesn't match any row for this tenant
	}
	if err := b.checkQuota(ctx, w.TeamID, *preset); err != nil {
		return err
	}
	w.CPURequest, w.MemoryRequest, w.GPURequest, w.StorageRequest = preset.CPURequest, preset.MemoryRequest, preset.GPURequest, preset.Storage
	w.Status = WorkspacePending
	if err := b.repo.Create(ctx, w); err != nil {
		return err
	}
	ns, err := b.namespaceFor(ctx, w.TenantID)
	if err != nil {
		return err
	}
	go b.provisioner.Create(context.Background(), ns, w, b.statusCallback(w))
	return nil
}

// startAsync re-starts an existing (Stopped) workspace by scaling its
// StatefulSet back up — a different provisioner call than Create, since
// Create would try to make a StatefulSet that already exists.
func (b *WorkspaceBiz) startAsync(tenantNamespace string, w *Workspace) {
	go b.provisioner.Start(context.Background(), tenantNamespace, w, b.statusCallback(w))
}

// statusCallback persists whatever the provisioner reports back to
// Postgres — shared by Create and startAsync, since both end the same way:
// the Pod eventually becomes Running or Failed, asynchronously.
func (b *WorkspaceBiz) statusCallback(w *Workspace) func(status WorkspaceStatus, errMsg string, podName string) {
	return func(status WorkspaceStatus, errMsg string, podName string) {
		w.Status = status
		w.ErrorMessage = errMsg
		w.K8sPodName = podName
		_ = b.repo.Update(context.Background(), w)
	}
}

func isKnownImage(key string) bool {
	for _, k := range ImageKeys {
		if k == key {
			return true
		}
	}
	return false
}

func (b *WorkspaceBiz) Get(ctx context.Context, id string) (*Workspace, error) {
	return b.repo.Get(ctx, id)
}

func (b *WorkspaceBiz) ListByOwner(ctx context.Context, ownerUserID string) ([]*Workspace, error) {
	return b.repo.ListByOwner(ctx, ownerUserID)
}

func (b *WorkspaceBiz) ListByTenant(ctx context.Context, tenantID string) ([]*Workspace, error) {
	return b.repo.ListByTenant(ctx, tenantID)
}

func (b *WorkspaceBiz) requireOwner(w *Workspace, callerUserID string) error {
	if w.OwnerUserID != callerUserID {
		return ErrNotWorkspaceOwner
	}
	return nil
}

func (b *WorkspaceBiz) Stop(ctx context.Context, id, callerUserID string) error {
	w, err := b.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := b.requireOwner(w, callerUserID); err != nil {
		return err
	}
	ns, err := b.namespaceFor(ctx, w.TenantID)
	if err != nil {
		return err
	}
	if err := b.provisioner.Stop(ctx, ns, w); err != nil {
		return err
	}
	now := time.Now()
	w.Status = WorkspaceStopped
	w.StoppedAt = &now
	return b.repo.Update(ctx, w)
}

// Start re-validates team quota (a stopped workspace doesn't count as
// "active", so the team's remaining quota may have changed while it was
// stopped) before scaling the StatefulSet back up.
func (b *WorkspaceBiz) Start(ctx context.Context, id, callerUserID string) error {
	w, err := b.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := b.requireOwner(w, callerUserID); err != nil {
		return err
	}
	preset, err := b.presets.Get(ctx, w.TenantID, w.Size)
	if err != nil {
		return err
	}
	if err := b.checkQuota(ctx, w.TeamID, *preset); err != nil {
		return err
	}
	w.Status = WorkspacePending
	w.ErrorMessage = ""
	if err := b.repo.Update(ctx, w); err != nil {
		return err
	}
	ns, err := b.namespaceFor(ctx, w.TenantID)
	if err != nil {
		return err
	}
	b.startAsync(ns, w)
	return nil
}

func (b *WorkspaceBiz) Delete(ctx context.Context, id, callerUserID string) error {
	w, err := b.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := b.requireOwner(w, callerUserID); err != nil {
		return err
	}
	return b.deleteWorkspace(ctx, w)
}

// deleteWorkspace skips the ownership check — used by UserBiz when deleting
// a user cascades to deleting their owned workspaces.
func (b *WorkspaceBiz) deleteWorkspace(ctx context.Context, w *Workspace) error {
	ns, err := b.namespaceFor(ctx, w.TenantID)
	if err != nil {
		return err
	}
	if err := b.provisioner.Delete(ctx, ns, w); err != nil {
		return err
	}
	return b.repo.Delete(ctx, w.ID)
}

// StopActiveForOwner stops all of ownerUserID's pending/running workspaces.
// Used by UserBiz.Disable — no per-call ownership check since the caller
// (a tenant_admin disabling a user) isn't the workspace owner.
func (b *WorkspaceBiz) StopActiveForOwner(ctx context.Context, ownerUserID string) error {
	active, err := b.repo.ListActiveByOwner(ctx, ownerUserID)
	if err != nil {
		return err
	}
	for _, w := range active {
		ns, err := b.namespaceFor(ctx, w.TenantID)
		if err != nil {
			return err
		}
		if err := b.provisioner.Stop(ctx, ns, w); err != nil {
			return err
		}
		now := time.Now()
		w.Status = WorkspaceStopped
		w.StoppedAt = &now
		if err := b.repo.Update(ctx, w); err != nil {
			return err
		}
	}
	return nil
}

// DeleteAllForOwner deletes all of ownerUserID's workspaces (K8s + DB row).
// Used by UserBiz.Delete before the user row itself is removed.
func (b *WorkspaceBiz) DeleteAllForOwner(ctx context.Context, ownerUserID string) error {
	owned, err := b.repo.ListByOwner(ctx, ownerUserID)
	if err != nil {
		return err
	}
	for _, w := range owned {
		if err := b.deleteWorkspace(ctx, w); err != nil {
			return err
		}
	}
	return nil
}
