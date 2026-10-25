package k8s

import (
	"context"
	"fmt"
	"log"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"tenant-service/backend/internal/biz"
)

// NoopTestJobProvisioner is used when K8s isn't reachable (local dev). It
// simulates a passed run after a short delay, same role as
// NoopWorkspaceProvisioner.
type NoopTestJobProvisioner struct{}

func (NoopTestJobProvisioner) Run(_ context.Context, ns string, hook *biz.TestHook, run *biz.TestRun, onStatus TestStatusFunc) {
	log.Printf("[k8s noop] would run test job image=%s cmd=%v in ns=%s", hook.TestImage, hook.TestCommand, ns)
	time.Sleep(500 * time.Millisecond)
	name := testJobName(run)
	onStatus(biz.TestRunPassed, "", name, name+"-xxxxx")
}

func (NoopTestJobProvisioner) FetchLogs(_ context.Context, _, _ string, _ int64) (string, error) {
	return "[k8s noop] no real logs available locally\n", nil
}

// TestStatusFunc reports a test run's terminal (or intermediate) status back
// to TestHookBiz, which persists it to Postgres.
type TestStatusFunc = func(status biz.TestRunStatus, errMsg, jobName, podName string)

// ClientGoTestJobProvisioner runs one batchv1.Job per trigger in the
// tenant's namespace — this codebase's first use of Job; every other
// workload (workspaces) is a long-running StatefulSet. BackoffLimit is 0
// (no automatic retries — a flaky retry would double-count "failed" history
// confusingly; a tenant wanting retries handles that in their own test
// script). The Job carries the same Kueue queue-name annotation workspaces
// use, so test runs are admitted through the same per-tenant LocalQueue.
type ClientGoTestJobProvisioner struct {
	clientset *kubernetes.Clientset
	timeout   time.Duration
}

func NewClientGoTestJobProvisioner(clientset *kubernetes.Clientset, timeout time.Duration) *ClientGoTestJobProvisioner {
	return &ClientGoTestJobProvisioner{clientset: clientset, timeout: timeout}
}

func testJobName(run *biz.TestRun) string { return fmt.Sprintf("test-%s", run.ID) }

func (p *ClientGoTestJobProvisioner) Run(ctx context.Context, tenantNamespace string, hook *biz.TestHook, run *biz.TestRun, onStatus TestStatusFunc) {
	ns := namespaceSlug(tenantNamespace)
	name := testJobName(run)

	backoffLimit := int32(0)
	deadlineSeconds := int64(p.timeout.Seconds())
	ttlSeconds := int32(3600)

	podSpec := corev1.PodSpec{
		RestartPolicy: corev1.RestartPolicyNever,
		Containers: []corev1.Container{{
			Name:    "test",
			Image:   hook.TestImage,
			Command: hook.TestCommand,
			Env: []corev1.EnvVar{
				{Name: "COMMIT_SHA", Value: run.CommitSHA},
				{Name: "IMAGE_TAG", Value: run.ImageTag},
			},
		}},
	}
	if hook.ImagePullSecret != "" {
		podSpec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: hook.ImagePullSecret}}
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			ActiveDeadlineSeconds:   &deadlineSeconds,
			TTLSecondsAfterFinished: &ttlSeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						// Same annotation buildStatefulSet uses — test Jobs are
						// queued through the tenant's existing LocalQueue, not a
						// separate unmanaged one.
						"kueue.x-k8s.io/queue-name": LocalQueueName(tenantNamespace),
					},
				},
				Spec: podSpec,
			},
		},
	}

	if _, err := p.clientset.BatchV1().Jobs(ns).Create(ctx, job, metav1.CreateOptions{}); err != nil {
		onStatus(biz.TestRunFailed, err.Error(), name, "")
		return
	}
	p.pollForCompletion(ctx, ns, name, onStatus)
}

func (p *ClientGoTestJobProvisioner) pollForCompletion(ctx context.Context, ns, jobName string, onStatus TestStatusFunc) {
	deadline := time.Now().Add(p.timeout)
	for time.Now().Before(deadline) {
		job, err := p.clientset.BatchV1().Jobs(ns).Get(ctx, jobName, metav1.GetOptions{})
		if err == nil {
			podName := firstJobPodName(ctx, p.clientset, ns, jobName)
			switch {
			case job.Status.Succeeded > 0:
				onStatus(biz.TestRunPassed, "", jobName, podName)
				return
			case job.Status.Failed > 0:
				onStatus(biz.TestRunFailed, "test job failed", jobName, podName)
				return
			}
		}
		time.Sleep(5 * time.Second)
	}
	onStatus(biz.TestRunFailed, "timed out waiting for test job to complete", jobName, "")
}

// firstJobPodName resolves the one Pod Kubernetes created for this Job —
// auto-labelled job-name=<jobName> — so FetchLogs has something to target.
// Best-effort: an empty result just means logs won't be fetchable yet/ever.
func firstJobPodName(ctx context.Context, clientset *kubernetes.Clientset, ns, jobName string) string {
	pods, err := clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + jobName})
	if err != nil || len(pods.Items) == 0 {
		return ""
	}
	return pods.Items[0].Name
}

func (p *ClientGoTestJobProvisioner) FetchLogs(ctx context.Context, tenantNamespace, podName string, tailLines int64) (string, error) {
	if podName == "" {
		return "", fmt.Errorf("no pod recorded for this run yet")
	}
	ns := namespaceSlug(tenantNamespace)
	req := p.clientset.CoreV1().Pods(ns).GetLogs(podName, &corev1.PodLogOptions{TailLines: &tailLines})
	stream, err := req.Stream(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("pod logs unavailable (likely garbage-collected): %w", err)
		}
		return "", err
	}
	defer stream.Close()

	buf := make([]byte, 0, 4096)
	chunk := make([]byte, 4096)
	for {
		n, err := stream.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
		}
		if err != nil {
			break
		}
	}
	return string(buf), nil
}
