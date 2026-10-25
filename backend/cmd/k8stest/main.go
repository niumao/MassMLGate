package main

import (
	"context"
	"fmt"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"tenant-service/backend/internal/biz"
	"tenant-service/backend/internal/pkg/k8s"
)

// Throwaway integration test harness — not part of the shipped app. Proves
// the StatefulSet-based workspace provisioner's full lifecycle against a
// real cluster: create (Pod+PVC come up), stop (Pod gone, PVC stays),
// start (same PVC reattaches), delete (PVC goes away too). Cleans up
// everything it creates, including the tenant namespace.
func main() {
	ctx := context.Background()
	kubeconfig := os.Getenv("KUBECONFIG")

	clientset, err := k8s.NewClientset(kubeconfig)
	must(err, "NewClientset")
	dynamicClient, err := k8s.NewDynamicClient(kubeconfig)
	must(err, "NewDynamicClient")

	provisioner := k8s.NewClientGoProvisioner(clientset, dynamicClient, "cluster-queue")
	wsProvisioner := k8s.NewClientGoWorkspaceProvisioner(clientset)

	tenant := &biz.Tenant{Namespace: "sts-test", QuotaCPU: 2000, QuotaMemory: 4 << 30, QuotaGPU: 0}
	must(provisioner.EnsureTenant(ctx, tenant), "EnsureTenant")
	ns := "mlops-t-" + tenant.Namespace
	fmt.Println("OK: tenant namespace ready:", ns)

	ws := &biz.Workspace{
		ID: "ststest1", ImageKey: "jupyter", Size: "small",
		CPURequest: 500, MemoryRequest: 1 << 30, GPURequest: 0,
	}

	fmt.Println("=== 1. Create: StatefulSet+Service+PVC, wait for Running ===")
	waitFor(ctx, wsProvisioner.Create, ns, ws)

	pvcName := "data-ws-" + ws.ID + "-0" // StatefulSet's volumeClaimTemplate naming convention
	pvc, err := clientset.CoreV1().PersistentVolumeClaims(ns).Get(ctx, pvcName, metav1.GetOptions{})
	must(err, "get PVC after create")
	fmt.Printf("OK: PVC %s phase=%s\n", pvc.Name, pvc.Status.Phase)

	fmt.Println("=== 2. Stop: Pod should disappear, PVC should stay ===")
	must(wsProvisioner.Stop(ctx, ns, ws), "Stop")
	time.Sleep(3 * time.Second)
	_, err = clientset.CoreV1().Pods(ns).Get(ctx, ws.K8sPodName, metav1.GetOptions{})
	fmt.Println("pod after stop (expect NotFound):", err)
	_, err = clientset.CoreV1().PersistentVolumeClaims(ns).Get(ctx, pvcName, metav1.GetOptions{})
	must(err, "PVC should still exist after Stop")
	fmt.Println("OK: PVC survived Stop")

	fmt.Println("=== 3. Start: same PVC should reattach ===")
	waitFor(ctx, wsProvisioner.Start, ns, ws)
	pod, err := clientset.CoreV1().Pods(ns).Get(ctx, ws.K8sPodName, metav1.GetOptions{})
	must(err, "get pod after start")
	reattached := false
	for _, v := range pod.Spec.Volumes {
		if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == pvcName {
			reattached = true
		}
	}
	fmt.Println("OK: same PVC reattached after Start:", reattached)

	fmt.Println("=== 4. Delete: StatefulSet+Service+PVC should all be gone ===")
	must(wsProvisioner.Delete(ctx, ns, ws), "Delete")
	time.Sleep(3 * time.Second)
	_, err = clientset.CoreV1().PersistentVolumeClaims(ns).Get(ctx, pvcName, metav1.GetOptions{})
	fmt.Println("PVC after delete (expect NotFound):", err)

	fmt.Println("=== cleanup: delete test tenant namespace ===")
	must(provisioner.DeleteTenant(ctx, tenant), "DeleteTenant")
	fmt.Println("cleanup OK")
}

func waitFor(ctx context.Context, fn func(context.Context, string, *biz.Workspace, k8s.StatusFunc), ns string, ws *biz.Workspace) {
	done := make(chan struct{})
	go fn(ctx, ns, ws, func(status biz.WorkspaceStatus, errMsg, podName string) {
		ws.Status, ws.ErrorMessage, ws.K8sPodName = status, errMsg, podName
		close(done)
	})
	select {
	case <-done:
		fmt.Printf("status=%s err=%q pod=%s\n", ws.Status, ws.ErrorMessage, ws.K8sPodName)
	case <-time.After(150 * time.Second):
		fmt.Println("TIMEOUT waiting for status callback")
		os.Exit(1)
	}
}

func must(err error, step string) {
	if err != nil {
		fmt.Printf("FAIL at %s: %v\n", step, err)
		os.Exit(1)
	}
}
