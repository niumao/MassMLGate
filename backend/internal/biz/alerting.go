package biz

import (
	"context"
	"errors"
)

type AlertReceiverType string

const (
	AlertReceiverSlack   AlertReceiverType = "slack"
	AlertReceiverWebhook AlertReceiverType = "webhook"
)

// AlertReceiver is where a firing alert gets sent. Target is the Slack
// incoming-webhook URL or the generic webhook callback URL, depending on
// Type — this editor doesn't support a custom routing tree, every receiver
// gets every alert (see AlertingProvisioner's single default route).
type AlertReceiver struct {
	Name   string
	Type   AlertReceiverType
	Target string
}

var (
	ErrInvalidAlertReceiverType    = errors.New("alert receiver type must be 'slack' or 'webhook'")
	ErrAlertReceiverNameRequired   = errors.New("alert receiver name is required")
	ErrAlertReceiverTargetRequired = errors.New("alert receiver target is required")
)

// AlertingProvisioner reads/writes the cluster's AlertmanagerConfig CRD —
// implemented by internal/pkg/k8s; a no-op (in-memory) implementation is
// used for local dev.
type AlertingProvisioner interface {
	GetReceivers(ctx context.Context) ([]AlertReceiver, error)
	SetReceivers(ctx context.Context, receivers []AlertReceiver) error
}

type AlertingBiz struct {
	provisioner AlertingProvisioner
}

func NewAlertingBiz(provisioner AlertingProvisioner) *AlertingBiz {
	return &AlertingBiz{provisioner: provisioner}
}

func (b *AlertingBiz) Get(ctx context.Context) ([]AlertReceiver, error) {
	return b.provisioner.GetReceivers(ctx)
}

func (b *AlertingBiz) Set(ctx context.Context, receivers []AlertReceiver) error {
	for _, r := range receivers {
		if r.Name == "" {
			return ErrAlertReceiverNameRequired
		}
		if r.Type != AlertReceiverSlack && r.Type != AlertReceiverWebhook {
			return ErrInvalidAlertReceiverType
		}
		if r.Target == "" {
			return ErrAlertReceiverTargetRequired
		}
	}
	return b.provisioner.SetReceivers(ctx, receivers)
}
