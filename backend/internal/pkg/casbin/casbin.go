package casbin

import (
	"context"

	casbinlib "github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"

	"tenant-service/backend/internal/biz"
)

// modelText implements RBAC-with-domains the way the design spec intends
// (tenant-scoped role permissions, plus a read-only drill-down for
// system_admin into any tenant) without the wildcard-domain footgun the
// spec's literal "p.dom == '*'" formulation had: domain membership lives
// entirely in g (g(user, role, domain)), so p rows are a plain, static
// role->permission mapping. The second matcher clause is what grants
// system_admin's permissions (and ONLY the permissions actually assigned to
// system_admin, e.g. team.read/workspace.read but never workspace.create)
// across every domain, by checking the user's role within the fixed
// "system" domain regardless of the request's actual domain.
const modelText = `
[request_definition]
r = sub, dom, obj, act

[policy_definition]
p = sub, obj, act

[role_definition]
g = _, _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub, r.dom) && r.obj == p.obj && r.act == p.act \
    || g(r.sub, p.sub, "system") && r.obj == p.obj && r.act == p.act
`

type Permission struct {
	Obj string
	Act string
}

// Permission sets, hardcoded per the design spec (not stored in the DB).
var (
	TenantManageAccess   = []Permission{{"tenant", "create"}, {"tenant", "read"}, {"tenant", "update"}, {"tenant", "delete"}}
	UserManageAccess     = []Permission{{"user", "create"}, {"user", "read"}, {"user", "update"}, {"user", "delete"}, {"user", "disable"}}
	TeamEditAccess       = []Permission{{"team", "create"}, {"team", "read"}, {"team", "update"}, {"team", "delete"}, {"team", "manage_members"}}
	TeamReadAccess       = []Permission{{"team", "read"}}
	QuotaEditAccess      = []Permission{{"quota", "read"}, {"quota", "allocate"}, {"quota", "update"}}
	WorkspaceEditAccess  = []Permission{{"workspace", "create"}, {"workspace", "read"}, {"workspace", "delete"}, {"workspace", "start"}, {"workspace", "stop"}}
	WorkspaceReadAccess  = []Permission{{"workspace", "read"}}
	MetricsReadAccess    = []Permission{{"metrics", "read"}}
	ClusterManageAccess  = []Permission{{"cluster", "manage"}}
	SizePresetEditAccess = []Permission{{"size_preset", "create"}, {"size_preset", "read"}, {"size_preset", "update"}, {"size_preset", "delete"}}
	SizePresetReadAccess = []Permission{{"size_preset", "read"}}
	TokenQuotaEditAccess = []Permission{{"token_quota", "create"}, {"token_quota", "read"}, {"token_quota", "update"}, {"token_quota", "delete"}}
	TokenQuotaReadAccess = []Permission{{"token_quota", "read"}}
	// TestHookEditAccess/TestHookReadAccess mirror the WorkspaceEditAccess/
	// WorkspaceReadAccess split: the workspace-owning member manages their
	// own test hooks (create/revoke/read), tenant_admin gets the same
	// tenant-wide read-only drill-down it gets for workspaces.
	TestHookEditAccess = []Permission{{"test_hook", "create"}, {"test_hook", "read"}, {"test_hook", "revoke"}}
	TestHookReadAccess = []Permission{{"test_hook", "read"}}
)

func menuPermission(key string) Permission { return Permission{"menu:" + key, "read"} }

// rolePermissions is the full role -> permission-set mapping from the
// design spec's Casbin RBAC section.
var rolePermissions = map[biz.Role][]Permission{
	biz.RoleSystemAdmin: concat(
		TenantManageAccess,
		QuotaEditAccess,
		MetricsReadAccess,
		TeamReadAccess,       // read-only drill-down only, not the rest of TeamEditAccess
		WorkspaceReadAccess,  // read-only drill-down
		TokenQuotaReadAccess, // read-only drill-down
		// UserManageAccess is a deliberate exception to the "read-only
		// drill-down" rule: System Admin needs a rescue path when a
		// tenant's only tenant_admin is disabled/locked out, and needs to
		// seed the first tenant_admin for a newly created tenant.
		UserManageAccess,
		ClusterManageAccess,
		[]Permission{menuPermission("dashboard"), menuPermission("tenants")},
	),
	biz.RoleTenantAdmin: concat(
		UserManageAccess,
		TeamEditAccess,
		QuotaEditAccess,
		WorkspaceReadAccess,
		MetricsReadAccess,
		SizePresetEditAccess,
		TokenQuotaEditAccess,
		TestHookReadAccess,
		[]Permission{menuPermission("dashboard"), menuPermission("users"), menuPermission("teams"), menuPermission("workspaces"), menuPermission("size_presets"), menuPermission("token_quotas"), menuPermission("test_runs")},
	),
	biz.RoleMember: concat(
		TeamReadAccess,
		WorkspaceEditAccess,
		SizePresetReadAccess,
		TestHookEditAccess,
		[]Permission{menuPermission("dashboard"), menuPermission("workspaces"), menuPermission("test_runs")},
	),
}

func concat(sets ...[]Permission) []Permission {
	var out []Permission
	for _, s := range sets {
		out = append(out, s...)
	}
	return out
}

// Domain returns the Casbin domain for a user: "system" for system_admin,
// otherwise "tenant:{tenantID}".
func Domain(role biz.Role, tenantID *string) string {
	if role == biz.RoleSystemAdmin {
		return "system"
	}
	return "tenant:" + *tenantID
}

type Enforcer struct {
	*casbinlib.Enforcer
}

func New() (*Enforcer, error) {
	m, err := model.NewModelFromString(modelText)
	if err != nil {
		return nil, err
	}
	e, err := casbinlib.NewEnforcer(m)
	if err != nil {
		return nil, err
	}
	return &Enforcer{e}, nil
}

// SyncUser replaces any existing grouping policy row for u.ID with one
// reflecting its current role/domain. Called after create/update so a
// user's permissions take effect immediately, without a server restart.
func (e *Enforcer) SyncUser(ctx context.Context, u *biz.User) error {
	if _, err := e.RemoveFilteredGroupingPolicy(0, u.ID); err != nil {
		return err
	}
	_, err := e.AddGroupingPolicy(u.ID, string(u.Role), Domain(u.Role, u.TenantID))
	return err
}

// RemoveUser deletes u's grouping policy row, called after a user is deleted.
func (e *Enforcer) RemoveUser(_ context.Context, userID string) error {
	_, err := e.RemoveFilteredGroupingPolicy(0, userID)
	return err
}

// Reload clears and rebuilds all policies from the hardcoded role
// permissions and the current set of users. Called once at startup.
func (e *Enforcer) Reload(ctx context.Context, users biz.UserRepo) error {
	e.ClearPolicy()
	for role, perms := range rolePermissions {
		for _, p := range perms {
			if _, err := e.AddPolicy(string(role), p.Obj, p.Act); err != nil {
				return err
			}
		}
	}
	all, err := users.ListAll(ctx)
	if err != nil {
		return err
	}
	for _, u := range all {
		dom := Domain(u.Role, u.TenantID)
		if _, err := e.AddGroupingPolicy(u.ID, string(u.Role), dom); err != nil {
			return err
		}
	}
	return nil
}
