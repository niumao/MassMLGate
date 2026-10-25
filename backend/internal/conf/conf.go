package conf

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Database   DatabaseConfig   `yaml:"database"`
	JWT        JWTConfig        `yaml:"jwt"`
	Bootstrap  BootstrapConfig  `yaml:"bootstrap"`
	K8s        K8sConfig        `yaml:"k8s"`
	Prometheus PrometheusConfig `yaml:"prometheus"`
	Alerting   AlertingConfig   `yaml:"alerting"`
	TestRuns   TestRunsConfig   `yaml:"test_runs"`
}

type ServerConfig struct {
	Addr string `yaml:"addr"`
	// StaticDir, if set, serves the frontend's `pnpm build` output (web/dist)
	// directly from this process — no separate nginx/reverse proxy needed for
	// a quick single-binary run. Empty = frontend not served (dev mode, Vite
	// dev server + proxy handles it instead).
	StaticDir string `yaml:"static_dir"`
}

type DatabaseConfig struct {
	DSN string `yaml:"dsn"`
}

type JWTConfig struct {
	Secret      string `yaml:"secret"`
	ExpiryHours int    `yaml:"expiry_hours"`
}

// BootstrapConfig seeds the root system_admin account on first startup if no
// system_admin user exists yet, and — separately — a default tenant with a
// tenant_admin and a member user if no tenant exists yet, so a fresh
// deployment has something to explore beyond the bare admin account.
type BootstrapConfig struct {
	AdminEmail    string `yaml:"admin_email"`
	AdminPassword string `yaml:"admin_password"`

	DefaultTenantName      string `yaml:"default_tenant_name"`
	DefaultTenantNamespace string `yaml:"default_tenant_namespace"`

	DefaultTenantAdminEmail    string `yaml:"default_tenant_admin_email"`
	DefaultTenantAdminPassword string `yaml:"default_tenant_admin_password"`

	DefaultMemberEmail    string `yaml:"default_member_email"`
	DefaultMemberPassword string `yaml:"default_member_password"`
}

// K8sConfig controls whether the tenant provisioning k8s client runs for
// real or is replaced by a no-op (for local dev without cluster access).
type K8sConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Kubeconfig string `yaml:"kubeconfig"`
	// ClusterQueueName is the cluster-wide Kueue ClusterQueue every
	// tenant's LocalQueue binds to. Set up once by platform ops (per the
	// GPU platform spec's "one ClusterQueue cohort shared across tenants"
	// decision) — Tenant Service only creates the per-tenant LocalQueue.
	ClusterQueueName string `yaml:"cluster_queue_name"`
}

// PrometheusConfig points at the cluster-wide Prometheus the GPU platform
// spec assumes is already deployed — Tenant Service only queries it, never
// deploys/manages it. Empty URL means "not configured"; utilization KPIs
// degrade to unavailable rather than erroring.
type PrometheusConfig struct {
	URL string `yaml:"url"`
}

// AlertingConfig controls where the admin page's alert-receiver editor
// writes its AlertmanagerConfig CRD (monitoring.coreos.com/v1alpha1) —
// assumes Prometheus Operator's Alertmanager is deployed with
// alertmanagerConfigSelector matching SelectorLabel=SelectorValue.
type AlertingConfig struct {
	Namespace     string `yaml:"namespace"`
	SelectorLabel string `yaml:"selector_label"`
	SelectorValue string `yaml:"selector_value"`
}

// TestRunsConfig bounds how long a tenant-triggered test Job is allowed to
// run before Tenant Service gives up polling it and marks the run Failed —
// the Job's own ActiveDeadlineSeconds is set to the same value as a second,
// independent enforcement inside the cluster. Needs tuning against real
// tenant test-suite runtimes; a too-short timeout will falsely fail a
// long-but-healthy run.
type TestRunsConfig struct {
	TimeoutMinutes int `yaml:"timeout_minutes"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
