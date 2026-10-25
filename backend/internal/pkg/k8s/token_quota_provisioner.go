package k8s

import (
	"context"
	"fmt"
	"log"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"tenant-service/backend/internal/biz"
)

// quotaPolicyGVR is agentrouter's (formerly "Envoy AI Gateway") QuotaPolicy
// CRD — the object that actually enforces a per-tenant token budget at the
// gateway, the way Kueue's LocalQueue (provisioner.go) enforces compute
// quota. UNVERIFIED against this cluster's actual installed CRD: the
// project recently rebranded from "Envoy AI Gateway" to "agent-router" and
// both the API group/version and the QuotaPolicy spec fields below are a
// best guess from its docs, not confirmed against `kubectl api-resources`
// or `kubectl explain quotapolicy.spec` here. Check both before relying on
// this in a real cluster.
var quotaPolicyGVR = schema.GroupVersionResource{Group: "aigateway.envoyproxy.io", Version: "v1alpha1", Resource: "quotapolicies"}

func quotaPolicyName(q *biz.TokenQuota) string {
	return fmt.Sprintf("tokenquota-%s", q.ID)
}

// NoopTokenQuotaProvisioner is used when K8s isn't reachable (local dev) or
// agentrouter isn't installed. The Postgres row still saves — this just
// skips the part that would make it actually enforced.
type NoopTokenQuotaProvisioner struct{}

func (NoopTokenQuotaProvisioner) EnsureQuota(_ context.Context, ns string, q *biz.TokenQuota) error {
	log.Printf("[k8s noop] would reconcile QuotaPolicy %s in ns=%s (model=%s limit=%d/%s)", quotaPolicyName(q), ns, q.ModelName, q.TokenLimit, q.Window)
	return nil
}

func (NoopTokenQuotaProvisioner) DeleteQuota(_ context.Context, ns string, q *biz.TokenQuota) error {
	log.Printf("[k8s noop] would delete QuotaPolicy %s in ns=%s", quotaPolicyName(q), ns)
	return nil
}

// ClientGoTokenQuotaProvisioner reconciles one QuotaPolicy CRD per
// TokenQuota, in the tenant's namespace, via the dynamic/unstructured
// client — same approach as ensureLocalQueue (provisioner.go) and for the
// same reason: no generated typed client is vendored for agentrouter's CRDs.
type ClientGoTokenQuotaProvisioner struct {
	dynamicClient dynamic.Interface
}

func NewClientGoTokenQuotaProvisioner(dynamicClient dynamic.Interface) *ClientGoTokenQuotaProvisioner {
	return &ClientGoTokenQuotaProvisioner{dynamicClient: dynamicClient}
}

func (p *ClientGoTokenQuotaProvisioner) EnsureQuota(ctx context.Context, ns string, q *biz.TokenQuota) error {
	name := quotaPolicyName(q)
	policy := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "aigateway.envoyproxy.io/v1alpha1",
			"kind":       "QuotaPolicy",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": ns,
			},
			"spec": map[string]interface{}{
				"model":      q.ModelName,
				"tokenLimit": q.TokenLimit,
				"window":     q.Window,
			},
		},
	}

	existing, err := p.dynamicClient.Resource(quotaPolicyGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = p.dynamicClient.Resource(quotaPolicyGVR).Namespace(ns).Create(ctx, policy, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	existing.Object["spec"] = policy.Object["spec"]
	_, err = p.dynamicClient.Resource(quotaPolicyGVR).Namespace(ns).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func (p *ClientGoTokenQuotaProvisioner) DeleteQuota(ctx context.Context, ns string, q *biz.TokenQuota) error {
	err := p.dynamicClient.Resource(quotaPolicyGVR).Namespace(ns).Delete(ctx, quotaPolicyName(q), metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
