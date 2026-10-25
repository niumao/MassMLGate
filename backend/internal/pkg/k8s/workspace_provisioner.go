package k8s

import (
	"context"
	"fmt"
	"log"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"

	"tenant-service/backend/internal/biz"
)

// Workspaces run as a single-replica StatefulSet, not a bare Pod. That one
// choice is what gives us two things for free, straight from Kubernetes'
// own built-in controllers — no custom code needed:
//
//   - Self-healing: if the Pod crashes while the StatefulSet wants 1
//     replica, Kubernetes recreates it automatically.
//   - Persistence ("save"): StatefulSet's volumeClaimTemplates guarantee
//     the *same* PVC reattaches to the Pod every time it (re)starts, so a
//     notebook's files survive a crash or an explicit Stop→Start.
//
// Stop/Start just scale replicas between 0 and 1 (the same lever Kubeflow's
// notebook controller uses for idle-culling) — Stop never deletes the PVC,
// so the data is still there on the next Start. Delete removes the
// StatefulSet with a retention policy that *does* delete the PVC, since
// deleting a workspace is meant to be permanent.

// NoopWorkspaceProvisioner is used when K8s isn't reachable (local dev). It
// simulates a successful provision after a short delay so the frontend has
// something to poll against.
type NoopWorkspaceProvisioner struct{}

func (NoopWorkspaceProvisioner) Create(_ context.Context, ns string, w *biz.Workspace, onStatus StatusFunc) {
	log.Printf("[k8s noop] would create workspace statefulset for %s in ns=%s", w.Name, ns)
	time.Sleep(500 * time.Millisecond)
	onStatus(biz.WorkspaceRunning, "", podName(w))
}

func (NoopWorkspaceProvisioner) Start(_ context.Context, ns string, w *biz.Workspace, onStatus StatusFunc) {
	log.Printf("[k8s noop] would scale workspace statefulset back to 1 replica for %s in ns=%s", w.Name, ns)
	time.Sleep(500 * time.Millisecond)
	onStatus(biz.WorkspaceRunning, "", podName(w))
}

func (NoopWorkspaceProvisioner) Stop(_ context.Context, ns string, w *biz.Workspace) error {
	log.Printf("[k8s noop] would scale workspace statefulset %s to 0 replicas in ns=%s", w.Name, ns)
	return nil
}

func (NoopWorkspaceProvisioner) Delete(_ context.Context, ns string, w *biz.Workspace) error {
	log.Printf("[k8s noop] would delete workspace statefulset+service+pvc for %s in ns=%s", w.Name, ns)
	return nil
}

// StatusFunc reports a workspace's status back to WorkspaceBiz, which
// persists it to Postgres. Shared by Create and Start since both end the
// same way: wait for the Pod to reach Running, or give up after a timeout.
type StatusFunc = func(status biz.WorkspaceStatus, errMsg string, podName string)

// ClientGoWorkspaceProvisioner creates/starts/stops/deletes a single
// StatefulSet+Service per workspace, in the tenant's namespace, queued
// through the tenant's Kueue LocalQueue. Polling for Running uses a bounded
// 5s/2min loop rather than a watch informer, per the design spec's
// deliberate simplification.
type ClientGoWorkspaceProvisioner struct {
	clientset *kubernetes.Clientset
}

func NewClientGoWorkspaceProvisioner(clientset *kubernetes.Clientset) *ClientGoWorkspaceProvisioner {
	return &ClientGoWorkspaceProvisioner{clientset: clientset}
}

var workspaceImages = map[string]string{
	"jupyter": "jupyter/base-notebook:latest",
	"vscode":  "codercom/code-server:latest",
}

// workDir is where each image expects its project files — this is what the
// data PVC gets mounted onto, so "save" actually persists the right thing.
var workDir = map[string]string{
	"jupyter": "/home/jovyan/work",
	"vscode":  "/home/coder/project",
}

func stsName(w *biz.Workspace) string { return fmt.Sprintf("ws-%s", w.ID) }
func svcName(w *biz.Workspace) string { return fmt.Sprintf("ws-%s", w.ID) }
func podName(w *biz.Workspace) string { return stsName(w) + "-0" } // StatefulSet's one-and-only replica
func namespaceSlug(ns string) string  { return fmt.Sprintf("mlops-t-%s", ns) }

func (p *ClientGoWorkspaceProvisioner) Create(ctx context.Context, tenantNamespace string, w *biz.Workspace, onStatus StatusFunc) {
	ns := namespaceSlug(tenantNamespace)

	sts := p.buildStatefulSet(ns, tenantNamespace, w, 1)
	if _, err := p.clientset.AppsV1().StatefulSets(ns).Create(ctx, sts, metav1.CreateOptions{}); err != nil {
		onStatus(biz.WorkspaceFailed, err.Error(), "")
		return
	}

	port := biz.ImagePorts[w.ImageKey]
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: svcName(w), Namespace: ns},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"workspace-id": w.ID},
			Ports:    []corev1.ServicePort{{Port: int32(port), TargetPort: intstr.FromInt(port)}},
		},
	}
	if _, err := p.clientset.CoreV1().Services(ns).Create(ctx, svc, metav1.CreateOptions{}); err != nil {
		onStatus(biz.WorkspaceFailed, err.Error(), podName(w))
		return
	}

	p.pollForRunning(ctx, ns, podName(w), onStatus)
}

// buildStatefulSet is the one place that defines what a workspace actually
// *is* in Kubernetes — reused by Create (replicas starts at desiredReplicas,
// always 1 there) and available for anyone reading this file to see the
// whole Pod/PVC shape in one spot, instead of split across Create/Start.
func (p *ClientGoWorkspaceProvisioner) buildStatefulSet(ns, tenantNamespace string, w *biz.Workspace, desiredReplicas int32) *appsv1.StatefulSet {
	port := biz.ImagePorts[w.ImageKey]
	labels := map[string]string{"app": "workspace", "workspace-id": w.ID}

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: stsName(w), Namespace: ns, Labels: labels},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &desiredReplicas,
			ServiceName: svcName(w),
			Selector:    &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						// This is the one line that makes the Pod Kueue-aware —
						// Kueue's admission webhook holds it until the tenant's
						// LocalQueue has room, per the Kueue model explained
						// alongside internal/pkg/k8s/provisioner.go.
						"kueue.x-k8s.io/queue-name": LocalQueueName(tenantNamespace),
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:         "workspace",
						Image:        workspaceImages[w.ImageKey],
						Ports:        []corev1.ContainerPort{{ContainerPort: int32(port)}},
						Resources:    corev1.ResourceRequirements{Requests: resourceList(w)},
						VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: workDir[w.ImageKey]}},
					}},
				},
			},
			// The actual "save" mechanism: Kubernetes creates one PVC per
			// StatefulSet replica and always reattaches the same one, so the
			// notebook's files outlive both crashes and explicit Stop/Start.
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{Name: "data"},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceStorage: *resource.NewQuantity(w.StorageRequest, resource.BinarySI)},
					},
				},
			}},
			// Stop (scale to 0) keeps the PVC — that's what makes Start bring
			// the same data back. Delete removes the StatefulSet entirely, and
			// this policy says the PVC — the saved data — goes with it.
			PersistentVolumeClaimRetentionPolicy: &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
				WhenDeleted: appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
				WhenScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
			},
		},
	}
}

func (p *ClientGoWorkspaceProvisioner) pollForRunning(ctx context.Context, ns, podName string, onStatus StatusFunc) {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		pod, err := p.clientset.CoreV1().Pods(ns).Get(ctx, podName, metav1.GetOptions{})
		if err == nil {
			switch pod.Status.Phase {
			case corev1.PodRunning:
				onStatus(biz.WorkspaceRunning, "", podName)
				return
			case corev1.PodFailed:
				onStatus(biz.WorkspaceFailed, podLastError(pod), podName)
				return
			}
		}
		time.Sleep(5 * time.Second)
	}
	onStatus(biz.WorkspaceFailed, "timed out waiting for pod to become Running", podName)
}

func podLastError(pod *corev1.Pod) string {
	if len(pod.Status.Conditions) > 0 {
		return pod.Status.Conditions[len(pod.Status.Conditions)-1].Message
	}
	return "pod failed"
}

// Start scales an *existing* StatefulSet back up to 1 replica — unlike
// Create, it never touches the Service or the StatefulSet's spec/PVC
// template, so the same PVC from before Stop reattaches unchanged.
func (p *ClientGoWorkspaceProvisioner) Start(ctx context.Context, tenantNamespace string, w *biz.Workspace, onStatus StatusFunc) {
	ns := namespaceSlug(tenantNamespace)
	if err := p.scale(ctx, ns, stsName(w), 1); err != nil {
		onStatus(biz.WorkspaceFailed, err.Error(), podName(w))
		return
	}
	p.pollForRunning(ctx, ns, podName(w), onStatus)
}

// Stop scales to 0 replicas — this is the only thing that actually frees
// the Pod's GPU/CPU/memory. The StatefulSet, Service, and PVC all stay.
func (p *ClientGoWorkspaceProvisioner) Stop(ctx context.Context, tenantNamespace string, w *biz.Workspace) error {
	return p.scale(ctx, namespaceSlug(tenantNamespace), stsName(w), 0)
}

func (p *ClientGoWorkspaceProvisioner) scale(ctx context.Context, ns, name string, replicas int32) error {
	sts, err := p.clientset.AppsV1().StatefulSets(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	sts.Spec.Replicas = &replicas
	_, err = p.clientset.AppsV1().StatefulSets(ns).Update(ctx, sts, metav1.UpdateOptions{})
	return err
}

// Delete removes the StatefulSet (cascading to the Pod and, per its
// PersistentVolumeClaimRetentionPolicy, the PVC/saved data) and the Service.
func (p *ClientGoWorkspaceProvisioner) Delete(ctx context.Context, tenantNamespace string, w *biz.Workspace) error {
	ns := namespaceSlug(tenantNamespace)
	if err := p.clientset.AppsV1().StatefulSets(ns).Delete(ctx, stsName(w), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err := p.clientset.CoreV1().Services(ns).Delete(ctx, svcName(w), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func resourceList(w *biz.Workspace) corev1.ResourceList {
	rl := corev1.ResourceList{
		corev1.ResourceCPU:    *resource.NewMilliQuantity(int64(w.CPURequest), resource.DecimalSI),
		corev1.ResourceMemory: *resource.NewQuantity(w.MemoryRequest, resource.BinarySI),
	}
	if w.GPURequest > 0 {
		rl["nvidia.com/gpu"] = *resource.NewQuantity(int64(w.GPURequest), resource.DecimalSI)
	}
	return rl
}
