package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"tenant-service/backend/internal/biz"
)

// alertmanagerConfigGVR targets Prometheus Operator's AlertmanagerConfig
// CRD. NOT live-validated against a real cluster (unlike the Kueue
// integration) — Prometheus Operator wasn't deployed in the cluster this
// was built against. Double-check `kubectl api-resources | grep alertmanager`
// for the actual installed version before relying on this; some deployments
// have moved this CRD to v1.
var alertmanagerConfigGVR = schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1alpha1", Resource: "alertmanagerconfigs"}

const (
	alertConfigName = "tenant-service-routes"
	alertSecretName = "tenant-service-alert-secrets"
)

// ClientGoAlertingProvisioner manages a single AlertmanagerConfig CR (name
// alertConfigName) in Namespace, with Slack webhook URLs stored in a
// companion Secret (AlertmanagerConfig requires SlackConfig.apiURL to be a
// secret reference, not an inline string — WebhookConfig.url can be
// inline). Requires the cluster's Alertmanager CR to have
// alertmanagerConfigSelector matching {SelectorLabel: SelectorValue}.
type ClientGoAlertingProvisioner struct {
	clientset     *kubernetes.Clientset
	dynamicClient dynamic.Interface
	namespace     string
	selectorLabel string
	selectorValue string
}

func NewClientGoAlertingProvisioner(clientset *kubernetes.Clientset, dynamicClient dynamic.Interface, namespace, selectorLabel, selectorValue string) *ClientGoAlertingProvisioner {
	return &ClientGoAlertingProvisioner{
		clientset: clientset, dynamicClient: dynamicClient,
		namespace: namespace, selectorLabel: selectorLabel, selectorValue: selectorValue,
	}
}

func (p *ClientGoAlertingProvisioner) GetReceivers(ctx context.Context) ([]biz.AlertReceiver, error) {
	obj, err := p.dynamicClient.Resource(alertmanagerConfigGVR).Namespace(p.namespace).Get(ctx, alertConfigName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	secret, err := p.clientset.CoreV1().Secrets(p.namespace).Get(ctx, alertSecretName, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}

	receivers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "receivers")
	var out []biz.AlertReceiver
	for _, r := range receivers {
		rm, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(rm, "name")
		if webhooks, _, _ := unstructured.NestedSlice(rm, "webhookConfigs"); len(webhooks) > 0 {
			if wh, ok := webhooks[0].(map[string]interface{}); ok {
				url, _, _ := unstructured.NestedString(wh, "url")
				out = append(out, biz.AlertReceiver{Name: name, Type: biz.AlertReceiverWebhook, Target: url})
			}
			continue
		}
		if slacks, _, _ := unstructured.NestedSlice(rm, "slackConfigs"); len(slacks) > 0 {
			target := ""
			if secret != nil {
				if v, ok := secret.Data[slackSecretKey(name)]; ok {
					target = string(v)
				}
			}
			out = append(out, biz.AlertReceiver{Name: name, Type: biz.AlertReceiverSlack, Target: target})
		}
	}
	return out, nil
}

func slackSecretKey(receiverName string) string { return fmt.Sprintf("slack-%s", receiverName) }

func (p *ClientGoAlertingProvisioner) SetReceivers(ctx context.Context, receiverList []biz.AlertReceiver) error {
	if len(receiverList) == 0 {
		return p.deleteConfig(ctx)
	}

	secretData := map[string][]byte{}
	var receiverObjs []interface{}
	for _, r := range receiverList {
		switch r.Type {
		case biz.AlertReceiverWebhook:
			receiverObjs = append(receiverObjs, map[string]interface{}{
				"name": r.Name,
				"webhookConfigs": []interface{}{
					map[string]interface{}{"url": r.Target},
				},
			})
		case biz.AlertReceiverSlack:
			key := slackSecretKey(r.Name)
			secretData[key] = []byte(r.Target)
			receiverObjs = append(receiverObjs, map[string]interface{}{
				"name": r.Name,
				"slackConfigs": []interface{}{
					map[string]interface{}{
						"apiURL": map[string]interface{}{
							"name": alertSecretName,
							"key":  key,
						},
					},
				},
			})
		}
	}

	if err := p.ensureSecret(ctx, secretData); err != nil {
		return err
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "monitoring.coreos.com/v1alpha1",
			"kind":       "AlertmanagerConfig",
			"metadata": map[string]interface{}{
				"name":      alertConfigName,
				"namespace": p.namespace,
				"labels":    map[string]interface{}{p.selectorLabel: p.selectorValue},
			},
			"spec": map[string]interface{}{
				"route": map[string]interface{}{
					"receiver": receiverList[0].Name,
				},
				"receivers": receiverObjs,
			},
		},
	}

	existing, err := p.dynamicClient.Resource(alertmanagerConfigGVR).Namespace(p.namespace).Get(ctx, alertConfigName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = p.dynamicClient.Resource(alertmanagerConfigGVR).Namespace(p.namespace).Create(ctx, obj, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	existing.Object["spec"] = obj.Object["spec"]
	existing.Object["metadata"].(map[string]interface{})["labels"] = obj.Object["metadata"].(map[string]interface{})["labels"]
	_, err = p.dynamicClient.Resource(alertmanagerConfigGVR).Namespace(p.namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func (p *ClientGoAlertingProvisioner) ensureSecret(ctx context.Context, data map[string][]byte) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: alertSecretName, Namespace: p.namespace},
		Data:       data,
	}
	existing, err := p.clientset.CoreV1().Secrets(p.namespace).Get(ctx, alertSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = p.clientset.CoreV1().Secrets(p.namespace).Create(ctx, secret, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	existing.Data = data
	_, err = p.clientset.CoreV1().Secrets(p.namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func (p *ClientGoAlertingProvisioner) deleteConfig(ctx context.Context) error {
	err := p.dynamicClient.Resource(alertmanagerConfigGVR).Namespace(p.namespace).Delete(ctx, alertConfigName, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	err = p.clientset.CoreV1().Secrets(p.namespace).Delete(ctx, alertSecretName, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// NoopAlertingProvisioner is used for local dev (k8s.enabled=false). It
// keeps receivers in memory for the session so the admin page's editor is
// at least internally consistent, without touching any cluster.
type NoopAlertingProvisioner struct {
	receivers []biz.AlertReceiver
}

func (p *NoopAlertingProvisioner) GetReceivers(_ context.Context) ([]biz.AlertReceiver, error) {
	return p.receivers, nil
}

func (p *NoopAlertingProvisioner) SetReceivers(_ context.Context, receivers []biz.AlertReceiver) error {
	p.receivers = receivers
	return nil
}
