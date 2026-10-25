package router

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"tenant-service/backend/internal/biz"
	"tenant-service/backend/internal/handler"
	pkgauth "tenant-service/backend/internal/pkg/auth"
	"tenant-service/backend/internal/pkg/casbin"
	"tenant-service/backend/internal/pkg/middleware"
)

type Handlers struct {
	Auth       *handler.AuthHandler
	Tenant     *handler.TenantHandler
	User       *handler.UserHandler
	Team       *handler.TeamHandler
	Metrics    *handler.MetricsHandler
	Workspace  *handler.WorkspaceHandler
	Catalog    *handler.CatalogHandler
	Cluster    *handler.ClusterHandler
	Alerting   *handler.AlertingHandler
	SizePreset *handler.SizePresetHandler
	TokenQuota *handler.TokenQuotaHandler
	TestHook   *handler.TestHookHandler
}

// New builds the gin.Engine. staticDir, if non-empty and it exists, serves
// the frontend's `pnpm build` output (web/dist) directly from the backend —
// so a single process serves both the UI and the API, no separate nginx/
// reverse proxy needed. Any path not matching /api/v1/* falls back to
// index.html for client-side routing; unmatched /api/v1/* paths still 404
// instead of serving HTML.
func New(h Handlers, issuer *pkgauth.JWTIssuer, users biz.UserRepo, enforcer *casbin.Enforcer, staticDir string) *gin.Engine {
	r := gin.Default()

	// Dev-only permissive CORS: the Vue dev server runs on a different
	// origin/port than the API. No cookies are used (Bearer JWT only), so
	// allowing all origins without credentials is safe here.
	r.Use(cors.New(cors.Config{
		AllowAllOrigins: true,
		AllowMethods:    []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:    []string{"Authorization", "Content-Type"},
		MaxAge:          12 * time.Hour,
	}))

	authMw := middleware.Auth(issuer, users)
	authz := func(obj, act string) gin.HandlerFunc { return middleware.Authz(enforcer, obj, act) }

	api := r.Group("/api/v1")

	api.POST("/auth/login", h.Auth.Login)

	// Test Hooks trigger: deliberately outside authMw/Casbin, same as
	// /auth/login above — this route's own security is the opaque,
	// high-entropy :token (hashed at rest, looked up by sha256 equality),
	// the same security posture as a Render/Vercel "Deploy Hook" secret URL.
	// Do NOT add authMw/authz here — an external CI caller has no JWT.
	api.POST("/hooks/:token/trigger", h.TestHook.Trigger)
	authGroup := api.Group("/auth", authMw)
	authGroup.POST("/logout", h.Auth.Logout)
	authGroup.GET("/me", h.Auth.Me)

	// System Admin
	tenants := api.Group("/tenants", authMw)
	tenants.POST("", authz("tenant", "create"), h.Tenant.Create)
	tenants.GET("", authz("tenant", "read"), h.Tenant.List)
	tenants.GET("/:id", authz("tenant", "read"), h.Tenant.Get)
	tenants.PATCH("/:id", authz("quota", "update"), h.Tenant.Update)
	tenants.DELETE("/:id", authz("tenant", "delete"), h.Tenant.Delete)
	tenants.GET("/:id/teams", authz("team", "read"), h.Team.ListForTenant)
	tenants.GET("/:id/workspaces", authz("workspace", "read"), h.Workspace.ListForTenant)
	// Rescue/bootstrap path: System Admin can list/create users in any
	// tenant (e.g. to recover a tenant whose only tenant_admin got
	// disabled, or to seed the first tenant_admin for a new tenant).
	tenants.GET("/:id/users", authz("user", "read"), h.User.ListForTenant)
	tenants.POST("/:id/users", authz("user", "create"), h.User.CreateForTenant)
	tenants.GET("/:id/token-quotas", authz("token_quota", "read"), h.TokenQuota.ListForTenant)

	metrics := api.Group("/metrics", authMw)
	metrics.GET("/cluster", authz("metrics", "read"), h.Metrics.Cluster)
	metrics.GET("/tenant", authz("metrics", "read"), h.Metrics.Tenant)

	// Tenant Admin
	usersGroup := api.Group("/users", authMw)
	usersGroup.POST("", authz("user", "create"), h.User.Create)
	usersGroup.GET("", authz("user", "read"), h.User.List)
	usersGroup.PATCH("/:user_id", authz("user", "update"), h.User.Update)
	usersGroup.DELETE("/:user_id", authz("user", "delete"), h.User.Delete)

	// Teams: create/update/delete/manage_members need TeamEditAccess
	// (tenant_admin only); read is shared with member via TeamReadAccess.
	teams := api.Group("/teams", authMw)
	teams.POST("", authz("team", "create"), h.Team.Create)
	teams.GET("", authz("team", "read"), h.Team.List)
	teams.GET("/:team_id", authz("team", "read"), h.Team.Get)
	teams.PATCH("/:team_id", authz("quota", "update"), h.Team.Update)
	teams.DELETE("/:team_id", authz("team", "delete"), h.Team.Delete)
	teams.GET("/:team_id/members", authz("team", "manage_members"), h.Team.ListMembers)
	teams.POST("/:team_id/members/:user_id", authz("team", "manage_members"), h.Team.AddMember)
	teams.DELETE("/:team_id/members/:user_id", authz("team", "manage_members"), h.Team.RemoveMember)

	// Member (own workspaces, via WorkspaceEditAccess) + Tenant Admin
	// (tenant-wide read, via WorkspaceReadAccess) — List/Get are shared,
	// Create/Start/Stop/Delete are member-only (enforced by Casbin: only
	// `member` holds WorkspaceEditAccess).
	workspaces := api.Group("/workspaces", authMw)
	workspaces.POST("", authz("workspace", "create"), h.Workspace.Create)
	workspaces.GET("", authz("workspace", "read"), h.Workspace.List)
	workspaces.GET("/:id", authz("workspace", "read"), h.Workspace.Get)
	workspaces.POST("/:id/start", authz("workspace", "start"), h.Workspace.Start)
	workspaces.POST("/:id/stop", authz("workspace", "stop"), h.Workspace.Stop)
	workspaces.DELETE("/:id", authz("workspace", "delete"), h.Workspace.Delete)

	// Test Hooks: the owning member manages their own workspace's hook
	// (create/revoke, TestHookEditAccess — mirrors WorkspaceEditAccess);
	// tenant_admin gets tenant-wide read-only history (TestHookReadAccess —
	// mirrors WorkspaceReadAccess). The actual trigger endpoint external CI
	// calls lives outside this group entirely — see api.POST("/hooks/...")
	// above.
	workspaces.GET("/:id/test-hooks", authz("test_hook", "read"), h.TestHook.List)
	workspaces.POST("/:id/test-hooks", authz("test_hook", "create"), h.TestHook.Create)
	workspaces.POST("/:id/test-hooks/:hookId/revoke", authz("test_hook", "revoke"), h.TestHook.Revoke)
	workspaces.GET("/:id/test-hooks/:hookId/runs", authz("test_hook", "read"), h.TestHook.ListRuns)
	workspaces.GET("/:id/test-hooks/:hookId/runs/:runId/logs", authz("test_hook", "read"), h.TestHook.RunLogs)

	api.GET("/workspace-catalog", authMw, authz("workspace", "read"), h.Catalog.Get)

	// Size presets: tenant_admin manages their tenant's catalog
	// (SizePresetEditAccess); member gets read-only access too
	// (SizePresetReadAccess) for parity with TeamReadAccess, even though in
	// practice it reaches the catalog via /workspace-catalog, not this
	// endpoint directly.
	sizePresets := api.Group("/size-presets", authMw)
	sizePresets.GET("", authz("size_preset", "read"), h.SizePreset.List)
	sizePresets.POST("", authz("size_preset", "create"), h.SizePreset.Create)
	sizePresets.PATCH("/:id", authz("size_preset", "update"), h.SizePreset.Update)
	sizePresets.DELETE("/:id", authz("size_preset", "delete"), h.SizePreset.Delete)

	// Token quotas: tenant_admin manages their tenant's per-model token
	// budgets, enforced by the AI gateway (agentrouter) rather than Tenant
	// Service itself — see biz.TokenQuotaProvisioner. system_admin gets the
	// same read-only drill-down pattern as workspaces/teams above.
	tokenQuotas := api.Group("/token-quotas", authMw)
	tokenQuotas.GET("", authz("token_quota", "read"), h.TokenQuota.List)
	tokenQuotas.POST("", authz("token_quota", "create"), h.TokenQuota.Create)
	tokenQuotas.PATCH("/:id", authz("token_quota", "update"), h.TokenQuota.Update)
	tokenQuotas.DELETE("/:id", authz("token_quota", "delete"), h.TokenQuota.Delete)

	// System Admin: one-time (or repeatable) cluster-wide Kueue bootstrap —
	// see deploy/kueue-bootstrap.yaml for the equivalent kubectl-apply path.
	api.POST("/cluster/kueue-setup", authMw, authz("cluster", "manage"), h.Cluster.KueueSetup)
	api.GET("/cluster/alerting", authMw, authz("cluster", "manage"), h.Alerting.Get)
	api.PUT("/cluster/alerting", authMw, authz("cluster", "manage"), h.Alerting.Set)

	if staticDir != "" {
		if absStatic, err := filepath.Abs(staticDir); err == nil {
			if info, err := os.Stat(absStatic); err == nil && info.IsDir() {
				r.NoRoute(serveSPA(absStatic))
			}
		}
	}

	return r
}

// serveSPA serves any file that actually exists under absStaticDir, and
// falls back to index.html for anything else (client-side routing) — except
// /api/ paths, which 404 instead of getting HTML. absStaticDir must already
// be absolute; every resolved path is re-checked against it to rule out
// path traversal via "../" in the request path.
func serveSPA(absStaticDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Status(http.StatusNotFound)
			return
		}
		requested := filepath.Join(absStaticDir, filepath.Clean("/"+c.Request.URL.Path))
		if !strings.HasPrefix(requested, absStaticDir) {
			c.Status(http.StatusForbidden)
			return
		}
		if info, err := os.Stat(requested); err == nil && !info.IsDir() {
			c.File(requested)
			return
		}
		c.File(filepath.Join(absStaticDir, "index.html"))
	}
}
