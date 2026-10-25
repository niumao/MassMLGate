package main

import (
	"context"
	"log"
	"time"

	"tenant-service/backend/internal/biz"
	"tenant-service/backend/internal/conf"
	"tenant-service/backend/internal/data"
	"tenant-service/backend/internal/handler"
	pkgauth "tenant-service/backend/internal/pkg/auth"
	"tenant-service/backend/internal/pkg/casbin"
	"tenant-service/backend/internal/pkg/k8s"
	"tenant-service/backend/internal/pkg/metrics"
	"tenant-service/backend/internal/router"
)

func main() {
	cfg, err := conf.Load("configs/config.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	d, err := data.New(cfg.Database.DSN)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	if err := d.Migrate(); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	tenantRepo := data.NewTenantRepo(d)
	userRepo := data.NewUserRepo(d)
	teamRepo := data.NewTeamRepo(d)
	workspaceRepo := data.NewWorkspaceRepo(d)
	sizePresetRepo := data.NewSizePresetRepo(d)
	tokenQuotaRepo := data.NewTokenQuotaRepo(d)
	testHookRepo := data.NewTestHookRepo(d)
	testRunRepo := data.NewTestRunRepo(d)

	enforcer, err := casbin.New()
	if err != nil {
		log.Fatalf("init casbin: %v", err)
	}

	tenantProvisioner, workspaceProvisioner, clusterProvisioner, capacityProvider, alertingProvisioner, tokenQuotaProvisioner, testJobProvisioner := newProvisioners(cfg.K8s, cfg.Alerting, cfg.TestRuns)
	tenantBiz := biz.NewTenantBiz(tenantRepo, tenantProvisioner)
	teamBiz := biz.NewTeamBiz(teamRepo, tenantRepo)
	workspaceBiz := biz.NewWorkspaceBiz(workspaceRepo, teamRepo, tenantRepo, sizePresetRepo, workspaceProvisioner)
	userBiz := biz.NewUserBiz(userRepo, enforcer, workspaceBiz)
	promClient := metrics.New(cfg.Prometheus.URL)
	clusterBiz := biz.NewClusterBiz(clusterProvisioner, capacityProvider, promClient, tenantRepo)
	alertingBiz := biz.NewAlertingBiz(alertingProvisioner)
	sizePresetBiz := biz.NewSizePresetBiz(sizePresetRepo)
	tokenQuotaBiz := biz.NewTokenQuotaBiz(tokenQuotaRepo, tenantRepo, tokenQuotaProvisioner)
	testHookBiz := biz.NewTestHookBiz(testHookRepo, testRunRepo, workspaceRepo, tenantRepo, testJobProvisioner)

	hasher := pkgauth.BcryptHasher{}
	authBiz := biz.NewAuthBiz(userRepo, hasher)
	issuer := pkgauth.NewJWTIssuer(cfg.JWT.Secret, cfg.JWT.ExpiryHours)

	ctx := context.Background()
	if err := seedRootAdmin(ctx, userRepo, hasher, cfg.Bootstrap); err != nil {
		log.Fatalf("seed root admin: %v", err)
	}
	if err := seedDefaultTenant(ctx, tenantBiz, teamBiz, userRepo, hasher, cfg.Bootstrap); err != nil {
		log.Fatalf("seed default tenant: %v", err)
	}

	if err := enforcer.Reload(ctx, userRepo); err != nil {
		log.Fatalf("load casbin policies: %v", err)
	}

	handlers := router.Handlers{
		Auth:       handler.NewAuthHandler(authBiz, userRepo, issuer, enforcer),
		Tenant:     handler.NewTenantHandler(tenantBiz),
		User:       handler.NewUserHandler(userBiz, hasher),
		Team:       handler.NewTeamHandler(teamBiz),
		Metrics:    handler.NewMetricsHandler(tenantBiz, userBiz, teamBiz, clusterBiz),
		Workspace:  handler.NewWorkspaceHandler(workspaceBiz, tenantBiz),
		Catalog:    handler.NewCatalogHandler(teamBiz, sizePresetBiz),
		Cluster:    handler.NewClusterHandler(clusterBiz),
		Alerting:   handler.NewAlertingHandler(alertingBiz),
		SizePreset: handler.NewSizePresetHandler(sizePresetBiz),
		TokenQuota: handler.NewTokenQuotaHandler(tokenQuotaBiz),
		TestHook:   handler.NewTestHookHandler(testHookBiz),
	}
	r := router.New(handlers, issuer, userRepo, enforcer, cfg.Server.StaticDir)

	log.Printf("listening on %s", cfg.Server.Addr)
	if err := r.Run(cfg.Server.Addr); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}

func newProvisioners(k8sCfg conf.K8sConfig, alertingCfg conf.AlertingConfig, testRunsCfg conf.TestRunsConfig) (biz.Provisioner, biz.WorkspaceProvisioner, biz.ClusterProvisioner, biz.CapacityProvider, biz.AlertingProvisioner, biz.TokenQuotaProvisioner, biz.TestJobProvisioner) {
	noops := func() (biz.Provisioner, biz.WorkspaceProvisioner, biz.ClusterProvisioner, biz.CapacityProvider, biz.AlertingProvisioner, biz.TokenQuotaProvisioner, biz.TestJobProvisioner) {
		return k8s.NoopProvisioner{}, k8s.NoopWorkspaceProvisioner{}, k8s.NoopProvisioner{}, k8s.NoopProvisioner{}, &k8s.NoopAlertingProvisioner{}, k8s.NoopTokenQuotaProvisioner{}, k8s.NoopTestJobProvisioner{}
	}
	if !k8sCfg.Enabled {
		log.Println("k8s provisioning disabled (k8s.enabled=false) — using no-op provisioners")
		return noops()
	}
	clientset, err := k8s.NewClientset(k8sCfg.Kubeconfig)
	if err != nil {
		log.Printf("k8s client init failed (%v) — falling back to no-op provisioners", err)
		return noops()
	}
	dynamicClient, err := k8s.NewDynamicClient(k8sCfg.Kubeconfig)
	if err != nil {
		log.Printf("k8s dynamic client init failed (%v) — falling back to no-op provisioners", err)
		return noops()
	}
	tenantProvisioner := k8s.NewClientGoProvisioner(clientset, dynamicClient, k8sCfg.ClusterQueueName)
	alertingProvisioner := k8s.NewClientGoAlertingProvisioner(clientset, dynamicClient, alertingCfg.Namespace, alertingCfg.SelectorLabel, alertingCfg.SelectorValue)
	tokenQuotaProvisioner := k8s.NewClientGoTokenQuotaProvisioner(dynamicClient)
	testJobProvisioner := k8s.NewClientGoTestJobProvisioner(clientset, time.Duration(testRunsCfg.TimeoutMinutes)*time.Minute)
	return tenantProvisioner, k8s.NewClientGoWorkspaceProvisioner(clientset), tenantProvisioner, tenantProvisioner, alertingProvisioner, tokenQuotaProvisioner, testJobProvisioner
}

// seedRootAdmin creates the bootstrap system_admin account if no user with
// that email exists yet. Idempotent, safe to run on every startup.
func seedRootAdmin(ctx context.Context, users *data.UserRepo, hasher biz.PasswordHasher, cfg conf.BootstrapConfig) error {
	if _, err := users.GetByEmail(ctx, cfg.AdminEmail); err == nil {
		return nil // already seeded
	}
	hash, err := hasher.Hash(cfg.AdminPassword)
	if err != nil {
		return err
	}
	admin := &biz.User{
		Email:        cfg.AdminEmail,
		PasswordHash: hash,
		DisplayName:  "Root Admin",
		Role:         biz.RoleSystemAdmin,
		Status:       "active",
	}
	return users.Create(ctx, admin)
}

// seedDefaultTenant creates a default tenant — with a team, a tenant_admin,
// and a member already added to that team — if no tenant exists yet.
// Skipped entirely once any tenant exists, so it only ever runs on a
// genuinely fresh deployment (one admin-only install isn't enough reason to
// re-seed; an admin may have deleted the last tenant on purpose).
func seedDefaultTenant(ctx context.Context, tenants *biz.TenantBiz, teams *biz.TeamBiz, users *data.UserRepo, hasher biz.PasswordHasher, cfg conf.BootstrapConfig) error {
	existing, err := tenants.List(ctx)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}

	tenant := &biz.Tenant{
		Name:        cfg.DefaultTenantName,
		Namespace:   cfg.DefaultTenantNamespace,
		QuotaCPU:    4000,
		QuotaMemory: 8 << 30,
		QuotaGPU:    1,
	}
	if err := tenants.Create(ctx, tenant); err != nil {
		return err
	}

	team := &biz.Team{
		TenantID:    tenant.ID,
		Name:        "default-team",
		QuotaCPU:    2000,
		QuotaMemory: 4 << 30,
		QuotaGPU:    1,
	}
	if err := teams.Create(ctx, team); err != nil {
		return err
	}

	if _, err := seedTenantUser(ctx, users, hasher, cfg.DefaultTenantAdminEmail, cfg.DefaultTenantAdminPassword, "Default Tenant Admin", biz.RoleTenantAdmin, tenant.ID); err != nil {
		return err
	}
	member, err := seedTenantUser(ctx, users, hasher, cfg.DefaultMemberEmail, cfg.DefaultMemberPassword, "Default User", biz.RoleMember, tenant.ID)
	if err != nil {
		return err
	}
	return teams.AddMember(ctx, team.ID, member.ID)
}

func seedTenantUser(ctx context.Context, users *data.UserRepo, hasher biz.PasswordHasher, email, password, displayName string, role biz.Role, tenantID string) (*biz.User, error) {
	hash, err := hasher.Hash(password)
	if err != nil {
		return nil, err
	}
	u := &biz.User{
		TenantID:     &tenantID,
		Email:        email,
		PasswordHash: hash,
		DisplayName:  displayName,
		Role:         role,
		Status:       "active",
	}
	if err := users.Create(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}
