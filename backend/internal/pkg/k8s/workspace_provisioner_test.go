package k8s

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"tenant-service/backend/internal/biz"
)

// Regression test for: K8s API rejecting workspace Pod creation with
// "spec.containers[0].resources.limits: Required value: Limit must be set
// for non overcommitable resources" — nvidia.com/gpu is an extended
// resource and the API requires limits == requests whenever it's
// requested at all, unlike cpu/memory which are fine request-only.
func TestResourceLimits_GPURequested_MatchesRequest(t *testing.T) {
	limits := resourceLimits(&biz.Workspace{GPURequest: 2})
	q, ok := limits["nvidia.com/gpu"]
	if !ok {
		t.Fatalf("Limits missing nvidia.com/gpu entry; got %v", limits)
	}
	if q.Value() != 2 {
		t.Fatalf("Limits[nvidia.com/gpu] = %v, want 2", q.Value())
	}
	if _, ok := limits[corev1.ResourceCPU]; ok {
		t.Fatalf("Limits should not constrain cpu (must stay burstable), got %v", limits)
	}
	if _, ok := limits[corev1.ResourceMemory]; ok {
		t.Fatalf("Limits should not constrain memory (must stay burstable), got %v", limits)
	}
}

func TestResourceLimits_NoGPU_Nil(t *testing.T) {
	if limits := resourceLimits(&biz.Workspace{GPURequest: 0}); limits != nil {
		t.Fatalf("Limits = %v, want nil when no GPU requested", limits)
	}
}

func TestBuildStatefulSet_GPUWorkspace_LimitsMatchRequestsForGPUOnly(t *testing.T) {
	p := &ClientGoWorkspaceProvisioner{}
	w := &biz.Workspace{ID: "ws-1", ImageKey: "jupyter", CPURequest: 500, MemoryRequest: 1 << 30, GPURequest: 1}

	sts := p.buildStatefulSet("ns-1", "tenant-1", w, 1)
	resources := sts.Spec.Template.Spec.Containers[0].Resources

	reqGPU, reqOK := resources.Requests["nvidia.com/gpu"]
	limGPU, limOK := resources.Limits["nvidia.com/gpu"]
	if !reqOK || !limOK {
		t.Fatalf("expected nvidia.com/gpu in both Requests and Limits, got requests=%v limits=%v", resources.Requests, resources.Limits)
	}
	if reqGPU.Value() != limGPU.Value() {
		t.Fatalf("GPU request (%v) must equal GPU limit (%v) — k8s rejects extended resources otherwise", reqGPU.Value(), limGPU.Value())
	}
}

func TestBuildStatefulSet_NoGPU_NoLimits(t *testing.T) {
	p := &ClientGoWorkspaceProvisioner{}
	w := &biz.Workspace{ID: "ws-2", ImageKey: "jupyter", CPURequest: 500, MemoryRequest: 1 << 30, GPURequest: 0}

	sts := p.buildStatefulSet("ns-1", "tenant-1", w, 1)
	if limits := sts.Spec.Template.Spec.Containers[0].Resources.Limits; limits != nil {
		t.Fatalf("Limits = %v, want nil for a non-GPU workspace", limits)
	}
}
