package data

import (
	"context"
	"database/sql"
	"errors"

	"tenant-service/backend/internal/biz"
)

type TestRunRepo struct {
	db *sql.DB
}

func NewTestRunRepo(d *Data) *TestRunRepo {
	return &TestRunRepo{db: d.DB}
}

const testRunColumns = `id, test_hook_id, workspace_id, tenant_id, commit_sha, image_tag, status, k8s_job_name, k8s_pod_name, error_message, created_at, updated_at, finished_at`

func (r *TestRunRepo) Create(ctx context.Context, run *biz.TestRun) error {
	return r.db.QueryRowContext(ctx, `
		INSERT INTO test_runs (test_hook_id, workspace_id, tenant_id, commit_sha, image_tag, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at`,
		run.TestHookID, run.WorkspaceID, run.TenantID, nullable(run.CommitSHA), nullable(run.ImageTag), string(run.Status),
	).Scan(&run.ID, &run.CreatedAt, &run.UpdatedAt)
}

func scanTestRun(scan func(dest ...any) error) (*biz.TestRun, error) {
	run := &biz.TestRun{}
	var commitSHA, imageTag, jobName, podName, errMsg sql.NullString
	var status string
	if err := scan(&run.ID, &run.TestHookID, &run.WorkspaceID, &run.TenantID, &commitSHA, &imageTag, &status,
		&jobName, &podName, &errMsg, &run.CreatedAt, &run.UpdatedAt, &run.FinishedAt); err != nil {
		return nil, err
	}
	run.CommitSHA, run.ImageTag, run.K8sJobName, run.K8sPodName, run.ErrorMessage = commitSHA.String, imageTag.String, jobName.String, podName.String, errMsg.String
	run.Status = biz.TestRunStatus(status)
	return run, nil
}

func (r *TestRunRepo) Update(ctx context.Context, run *biz.TestRun) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE test_runs SET status=$1, k8s_job_name=$2, k8s_pod_name=$3, error_message=$4, finished_at=$5, updated_at=now()
		WHERE id=$6`,
		string(run.Status), nullable(run.K8sJobName), nullable(run.K8sPodName), nullable(run.ErrorMessage), run.FinishedAt, run.ID,
	)
	return err
}

func (r *TestRunRepo) Get(ctx context.Context, id string) (*biz.TestRun, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+testRunColumns+` FROM test_runs WHERE id=$1`, id)
	run, err := scanTestRun(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, biz.ErrTestRunNotFound
	}
	return run, err
}

func (r *TestRunRepo) ListByHook(ctx context.Context, hookID string) ([]*biz.TestRun, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+testRunColumns+` FROM test_runs WHERE test_hook_id=$1 ORDER BY created_at DESC`, hookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*biz.TestRun
	for rows.Next() {
		run, err := scanTestRun(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}
