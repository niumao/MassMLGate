package k8s

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ClusterCapacity sums schedulable node capacity — the authoritative "how
// much does this cluster actually have" number, read directly from the
// Kubernetes API rather than Prometheus/kube-state-metrics, so it works
// even if those exporters aren't deployed.
type ClusterCapacity struct {
	CPUMilli int64
	Memory   int64
	GPU      int64
}

func GetClusterCapacity(ctx context.Context, clientset *kubernetes.Clientset) (ClusterCapacity, error) {
	var result ClusterCapacity
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return result, err
	}
	for _, n := range nodes.Items {
		if !isNodeReady(&n) || !isNodeSchedulable(&n) {
			continue
		}
		result.CPUMilli += n.Status.Allocatable.Cpu().MilliValue()
		result.Memory += n.Status.Allocatable.Memory().Value()
		if gpu, ok := n.Status.Allocatable["nvidia.com/gpu"]; ok {
			result.GPU += gpu.Value()
		}
	}
	return result, nil
}

// NodeCapacity is one node's allocatable capacity plus the readiness/
// schedulability flags GetClusterCapacity uses to decide whether to count
// it — surfaced per-node (rather than folded into the cluster-wide sum) so
// the dashboard can show e.g. a cordoned or NotReady node for what it is
// instead of silently dropping it from the total.
type NodeCapacity struct {
	Name        string
	Ready       bool
	Schedulable bool
	CPUMilli    int64
	Memory      int64
	GPU         int64
}

// GetNodeCapacities lists every node's allocatable capacity, unfiltered —
// callers that need the "actually usable" total should sum only the rows
// where Ready && Schedulable, same condition GetClusterCapacity applies
// internally.
func GetNodeCapacities(ctx context.Context, clientset *kubernetes.Clientset) ([]NodeCapacity, error) {
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	result := make([]NodeCapacity, 0, len(nodes.Items))
	for _, n := range nodes.Items {
		nc := NodeCapacity{
			Name:        n.Name,
			Ready:       isNodeReady(&n),
			Schedulable: isNodeSchedulable(&n),
			CPUMilli:    n.Status.Allocatable.Cpu().MilliValue(),
			Memory:      n.Status.Allocatable.Memory().Value(),
		}
		if gpu, ok := n.Status.Allocatable["nvidia.com/gpu"]; ok {
			nc.GPU = gpu.Value()
		}
		result = append(result, nc)
	}
	return result, nil
}

func isNodeReady(n *corev1.Node) bool {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// isNodeSchedulable excludes cordoned nodes and nodes with a NoSchedule/
// NoExecute taint tenant workloads have no toleration for — most notably
// the control-plane node, whose capacity is real but never actually
// available to run a tenant's Pods.
func isNodeSchedulable(n *corev1.Node) bool {
	if n.Spec.Unschedulable {
		return false
	}
	for _, t := range n.Spec.Taints {
		if t.Effect == corev1.TaintEffectNoSchedule || t.Effect == corev1.TaintEffectNoExecute {
			return false
		}
	}
	return true
}
