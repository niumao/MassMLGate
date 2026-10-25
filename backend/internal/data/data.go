package data

import (
	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Data struct {
	DB *sql.DB
}

func New(dsn string) (*Data, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return &Data{DB: db}, nil
}

// Migrate creates all tables if they don't already exist. Idempotent, safe to
// run on every startup — there's no migration framework in this skeleton.
func (d *Data) Migrate() error {
	_, err := d.DB.Exec(schemaSQL)
	return err
}

const schemaSQL = `
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS tenants (
	id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	name         text NOT NULL,
	namespace    text NOT NULL UNIQUE,
	quota_cpu    integer NOT NULL DEFAULT 0,
	quota_memory bigint NOT NULL DEFAULT 0,
	quota_gpu    integer NOT NULL DEFAULT 0,
	status       text NOT NULL DEFAULT 'active',
	created_at   timestamptz NOT NULL DEFAULT now(),
	updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
	id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id     uuid REFERENCES tenants(id) ON DELETE CASCADE,
	email         text NOT NULL UNIQUE,
	password_hash text NOT NULL,
	display_name  text NOT NULL,
	role          text NOT NULL,
	status        text NOT NULL DEFAULT 'active',
	created_at    timestamptz NOT NULL DEFAULT now(),
	updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS teams (
	id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
	name          text NOT NULL,
	quota_cpu     integer NOT NULL DEFAULT 0,
	quota_memory  bigint NOT NULL DEFAULT 0,
	quota_gpu     integer NOT NULL DEFAULT 0,
	created_at    timestamptz NOT NULL DEFAULT now(),
	updated_at    timestamptz NOT NULL DEFAULT now(),
	UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS team_members (
	team_id uuid NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
	user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	PRIMARY KEY (team_id, user_id)
);

CREATE TABLE IF NOT EXISTS workspaces (
	id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id       uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
	team_id         uuid NOT NULL REFERENCES teams(id),
	owner_user_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name            text NOT NULL,
	image_key       text NOT NULL,
	size            text NOT NULL,
	cpu_request     integer NOT NULL,
	memory_request  bigint NOT NULL,
	gpu_request     integer NOT NULL,
	status          text NOT NULL DEFAULT 'pending',
	k8s_pod_name     text,
	k8s_service_name text,
	error_message   text,
	created_at      timestamptz NOT NULL DEFAULT now(),
	updated_at      timestamptz NOT NULL DEFAULT now(),
	stopped_at      timestamptz
);

CREATE TABLE IF NOT EXISTS tenant_size_presets (
	id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id      uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
	name           text NOT NULL,
	cpu_request    integer NOT NULL,
	memory_request bigint NOT NULL,
	gpu_request    integer NOT NULL,
	storage        bigint NOT NULL,
	created_at     timestamptz NOT NULL DEFAULT now(),
	updated_at     timestamptz NOT NULL DEFAULT now(),
	UNIQUE (tenant_id, name),
	CHECK (cpu_request >= 0 AND memory_request >= 0 AND gpu_request >= 0 AND storage >= 0)
);

ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS storage_request bigint NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS token_quotas (
	id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id    uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
	model_name   text NOT NULL,
	token_limit  bigint NOT NULL,
	"window"     text NOT NULL DEFAULT 'daily',
	created_at   timestamptz NOT NULL DEFAULT now(),
	updated_at   timestamptz NOT NULL DEFAULT now(),
	UNIQUE (tenant_id, model_name),
	CHECK (token_limit >= 0),
	CHECK ("window" IN ('daily', 'monthly'))
);

CREATE TABLE IF NOT EXISTS test_hooks (
	id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	tenant_id         uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
	workspace_id      uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
	name              text NOT NULL,
	token_hash        text NOT NULL UNIQUE,
	test_image        text NOT NULL,
	test_command      jsonb NOT NULL DEFAULT '[]',
	image_pull_secret text,
	created_at        timestamptz NOT NULL DEFAULT now(),
	updated_at        timestamptz NOT NULL DEFAULT now(),
	revoked_at        timestamptz,
	last_triggered_at timestamptz
);

CREATE TABLE IF NOT EXISTS test_runs (
	id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	test_hook_id  uuid NOT NULL REFERENCES test_hooks(id) ON DELETE CASCADE,
	workspace_id  uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
	tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
	commit_sha    text,
	image_tag     text,
	status        text NOT NULL DEFAULT 'pending',
	k8s_job_name  text,
	k8s_pod_name  text,
	error_message text,
	created_at    timestamptz NOT NULL DEFAULT now(),
	updated_at    timestamptz NOT NULL DEFAULT now(),
	finished_at   timestamptz,
	CHECK (status IN ('pending', 'running', 'passed', 'failed'))
);
CREATE INDEX IF NOT EXISTS idx_test_runs_hook ON test_runs(test_hook_id, created_at DESC);
`
