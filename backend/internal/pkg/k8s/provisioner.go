package k8s

import (
	"context"
	"fmt"
	"log"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"tenant-service/backend/internal/biz"
)

// localQueueGVR is the Kueue LocalQueue CRD. Kueue's CRDs aren't part of
// the standard Kubernetes API groups, so there's no generated typed client
// for them vendored here — the dynamic/unstructured client is enough for
// the single "create if missing" operation needed. v1beta2 as of Kueue
// 0.11+; check `kubectl api-resources | grep kueue` against your cluster
// if LocalQueue creation starts failing after a Kueue upgrade/downgrade.
var localQueueGVR = schema.GroupVersionResource{Group: "kueue.x-k8s.io", Version: "v1beta2", Resource: "localqueues"}
var resourceFlavorGVR = schema.GroupVersionResource{Group: "kueue.x-k8s.io", Version: "v1beta2", Resource: "resourceflavors"}
var clusterQueueGVR = schema.GroupVersionResource{Group: "kueue.x-k8s.io", Version: "v1beta2", Resource: "clusterqueues"}

// defaultFlavorName is the single ResourceFlavor every tenant's workloads
// land in. One generic flavor (no node affinity) is enough for this
// deployment — Kueue doesn't need a flavor-level node selector to put
// GPU-requesting Pods on GPU nodes, the kubelet device plugin's extended
// resource accounting already does that.
const defaultFlavorName = "default-flavor"

// LocalQueueName is the per-tenant LocalQueue name, bound to the cluster's
// shared ClusterQueue cohort — also used by the workspace provisioner's
// kueue.x-k8s.io/queue-name Pod annotation, so the two must stay in sync.
func LocalQueueName(tenantNamespace string) string {
	return fmt.Sprintf("%s-queue", tenantNamespace)
}

// NoopProvisioner is used when K8s isn't reachable (local dev). It only
// logs — the tenant/team/user CRUD above it still works against Postgres.
type NoopProvisioner struct{}

func (NoopProvisioner) EnsureTenant(_ context.Context, t *biz.Tenant) error {
	log.Printf("[k8s noop] would reconcile namespace mlops-t-%s (cpu=%d memory=%d gpu=%d)", t.Namespace, t.QuotaCPU, t.QuotaMemory, t.QuotaGPU)
	return nil
}

func (NoopProvisioner) DeleteTenant(_ context.Context, t *biz.Tenant) error {
	log.Printf("[k8s noop] would delete namespace mlops-t-%s", t.Namespace)
	return nil
}

// GetCapacity returns small fixed fake numbers so the admin dashboard has
// something nonzero to render locally without a real cluster.
func (NoopProvisioner) GetCapacity(_ context.Context) (cpuMilli int64, memory int64, gpu int64, err error) {
	return 32000, 64 << 30, 4, nil
}

// ListNodes returns two fake nodes matching GetCapacity's totals, so the
// per-node dashboard view has something nonzero to render locally without a
// real cluster.
func (NoopProvisioner) ListNodes(_ context.Context) ([]biz.NodeCapacity, error) {
	return []biz.NodeCapacity{
		{Name: "noop-node-1", Ready: true, Schedulable: true, CPUMilli: 16000, Memory: 32 << 30, GPU: 2},
		{Name: "noop-node-2", Ready: true, Schedulable: true, CPUMilli: 16000, Memory: 32 << 30, GPU: 2},
	}, nil
}

func (NoopProvisioner) EnsureClusterQueue(_ context.Context, cpu int, memory int64, gpu int) error {
	log.Printf("[k8s noop] would ensure cluster-wide ResourceFlavor+ClusterQueue (cpu=%d memory=%d gpu=%d)", cpu, memory, gpu)
	return nil
}

// ClientGoProvisioner reconciles the tenant's namespace, ResourceQuota, and
// Kueue LocalQueue (bound to the cluster-wide ClusterQueue named by
// clusterQueueName — see the GPU platform spec's "one ClusterQueue cohort
// shared across tenants" decision; that ClusterQueue itself is cluster
// infrastructure set up once by platform ops, not managed here).
type ClientGoProvisioner struct {
	clientset        *kubernetes.Clientset
	dynamicClient    dynamic.Interface
	clusterQueueName string
}

func NewClientGoProvisioner(clientset *kubernetes.Clientset, dynamicClient dynamic.Interface, clusterQueueName string) *ClientGoProvisioner {
	return &ClientGoProvisioner{clientset: clientset, dynamicClient: dynamicClient, clusterQueueName: clusterQueueName}
}

// NewClientset builds the shared client-go Clientset used by both the
// tenant and workspace provisioners.
func NewClientset(kubeconfig string) (*kubernetes.Clientset, error) {
	cfg, err := loadRESTConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}

// NewDynamicClient builds the dynamic client used for Kueue's
// unstructured LocalQueue CRD.
func NewDynamicClient(kubeconfig string) (dynamic.Interface, error) {
	cfg, err := loadRESTConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	return dynamic.NewForConfig(cfg)
}

func loadRESTConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return rest.InClusterConfig()
}

func namespaceName(t *biz.Tenant) string {
	return fmt.Sprintf("mlops-t-%s", t.Namespace)
}

func (p *ClientGoProvisioner) EnsureTenant(ctx context.Context, t *biz.Tenant) error {
	ns := namespaceName(t)
	_, err := p.clientset.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = p.clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns},
		}, metav1.CreateOptions{})
	}
	if err != nil {
		return err
	}

	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{Name: "tenant-quota", Namespace: ns},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceRequestsCPU:    *resource.NewMilliQuantity(int64(t.QuotaCPU), resource.DecimalSI),
				corev1.ResourceRequestsMemory: *resource.NewQuantity(t.QuotaMemory, resource.BinarySI),
				"requests.nvidia.com/gpu":     *resource.NewQuantity(int64(t.QuotaGPU), resource.DecimalSI),
			},
		},
	}
	existing, err := p.clientset.CoreV1().ResourceQuotas(ns).Get(ctx, quota.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = p.clientset.CoreV1().ResourceQuotas(ns).Create(ctx, quota, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	existing.Spec = quota.Spec
	if _, err := p.clientset.CoreV1().ResourceQuotas(ns).Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
		return err
	}

	return p.ensureLocalQueue(ctx, ns, t.Namespace)
}

// ensureLocalQueue creates the tenant's LocalQueue if it doesn't already
// exist, in k8s namespace ns, named from the tenant's raw namespace slug
// (tenantSlug — must match the name the workspace provisioner's
// kueue.x-k8s.io/queue-name Pod annotation uses, via LocalQueueName).
// LocalQueue.spec.clusterQueue is immutable in Kueue, so this never updates
// an existing one — only the ClusterQueue binding at creation time matters,
// and that's fixed per deployment (clusterQueueName), not per tenant quota
// change.
func (p *ClientGoProvisioner) ensureLocalQueue(ctx context.Context, ns, tenantSlug string) error {
	name := LocalQueueName(tenantSlug)
	_, err := p.dynamicClient.Resource(localQueueGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	lq := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "kueue.x-k8s.io/v1beta2",
			"kind":       "LocalQueue",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": ns,
			},
			"spec": map[string]interface{}{
				"clusterQueue": p.clusterQueueName,
			},
		},
	}
	_, err = p.dynamicClient.Resource(localQueueGVR).Namespace(ns).Create(ctx, lq, metav1.CreateOptions{})
	return err
}

func (p *ClientGoProvisioner) DeleteTenant(ctx context.Context, t *biz.Tenant) error {
	err := p.clientset.CoreV1().Namespaces().Delete(ctx, namespaceName(t), metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// EnsureClusterQueue creates (or updates the quota on) the cluster-wide
// ResourceFlavor + ClusterQueue named p.clusterQueueName, that every
// tenant's LocalQueue binds to — the System-Admin-triggered equivalent of
// manually applying deploy/kueue-bootstrap.yaml. Unlike the per-tenant
// LocalQueue (whose spec.clusterQueue is immutable, so never updated),
// ClusterQueue.spec.resourceGroups *is* mutable — re-running this to
// change quota is expected to work.
func (p *ClientGoProvisioner) GetCapacity(ctx context.Context) (cpuMilli int64, memory int64, gpu int64, err error) {
	c, err := GetClusterCapacity(ctx, p.clientset)
	return c.CPUMilli, c.Memory, c.GPU, err
}

func (p *ClientGoProvisioner) ListNodes(ctx context.Context) ([]biz.NodeCapacity, error) {
	nodes, err := GetNodeCapacities(ctx, p.clientset)
	if err != nil {
		return nil, err
	}
	result := make([]biz.NodeCapacity, len(nodes))
	for i, n := range nodes {
		result[i] = biz.NodeCapacity{
			Name:        n.Name,
			Ready:       n.Ready,
			Schedulable: n.Schedulable,
			CPUMilli:    n.CPUMilli,
			Memory:      n.Memory,
			GPU:         n.GPU,
		}
	}
	return result, nil
}

func (p *ClientGoProvisioner) EnsureClusterQueue(ctx context.Context, cpu int, memory int64, gpu int) error {
	flavor := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "kueue.x-k8s.io/v1beta2",
			"kind":       "ResourceFlavor",
			"metadata":   map[string]interface{}{"name": defaultFlavorName},
			"spec":       map[string]interface{}{},
		},
	}
	if _, err := p.dynamicClient.Resource(resourceFlavorGVR).Create(ctx, flavor, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	cq := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "kueue.x-k8s.io/v1beta2",
			"kind":       "ClusterQueue",
			"metadata":   map[string]interface{}{"name": p.clusterQueueName},
			"spec": map[string]interface{}{
				"namespaceSelector": map[string]interface{}{},
				"resourceGroups": []interface{}{
					map[string]interface{}{
						"coveredResources": []interface{}{"cpu", "memory", "nvidia.com/gpu"},
						"flavors": []interface{}{
							map[string]interface{}{
								"name": defaultFlavorName,
								"resources": []interface{}{
									map[string]interface{}{"name": "cpu", "nominalQuota": fmt.Sprintf("%dm", cpu)},
									map[string]interface{}{"name": "memory", "nominalQuota": fmt.Sprintf("%d", memory)},
									map[string]interface{}{"name": "nvidia.com/gpu", "nominalQuota": fmt.Sprintf("%d", gpu)},
								},
							},
						},
					},
				},
			},
		},
	}

	existing, err := p.dynamicClient.Resource(clusterQueueGVR).Get(ctx, p.clusterQueueName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = p.dynamicClient.Resource(clusterQueueGVR).Create(ctx, cq, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	existing.Object["spec"] = cq.Object["spec"]
	_, err = p.dynamicClient.Resource(clusterQueueGVR).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}
