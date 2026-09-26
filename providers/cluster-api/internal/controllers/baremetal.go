package controllers

import (
	"context"
	"fmt"

	infrav1 "github.com/sugaf1204/gomi/providers/cluster-api/api/v1alpha1"
	"github.com/sugaf1204/gomi/providers/cluster-api/internal/gomi"
	"github.com/sugaf1204/gomi/providers/cluster-api/internal/sealedseed"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *MachineReconciler) reconcileBareMetal(ctx context.Context, api *gomi.Client, infra *infrav1.GomiMachine, m *clusterv1.Machine) (ctrl.Result, error) {
	spec := infra.Spec.BareMetal
	if spec == nil || spec.Pool == "" || spec.OSImageRef == "" || infra.Spec.VirtualMachine != nil {
		return r.report(ctx, infra, false, "InvalidSpec", "BareMetal requires pool and prepared OS image", nil)
	}
	if err := api.RequireBareMetal(ctx); err != nil {
		return r.report(ctx, infra, false, "UnsupportedServer", "Server must support sealed bare-metal bootstrap", err)
	}
	id := infra.Spec.InstanceID
	host, err := api.GetBareMetal(ctx, id)
	if gomi.IsStatus(err, 404) {
		if infra.Spec.ProviderID != nil {
			return r.report(ctx, infra, false, "InstanceMissing", "Previously provisioned claim disappeared; replace the Machine", nil)
		}
		// Avoid occupying scarce physical capacity before CABPK can supply bootstrap.
		if m.Spec.Bootstrap.DataSecretName == nil {
			return r.report(ctx, infra, false, "WaitingForBootstrap", "Waiting for bootstrap data Secret", nil)
		}
		if err := api.AcquireBareMetal(ctx, id, spec.Pool); err != nil {
			return r.report(ctx, infra, false, "WaitingForCapacity", "Cannot acquire a host from the enrolled pool", err)
		}
		return retry, nil
	}
	if err != nil {
		return r.report(ctx, infra, false, "GomiUnavailable", "Cannot read bare-metal claim", err)
	}
	if host.Owner != id || host.Pool != spec.Pool {
		return r.report(ctx, infra, false, "OwnershipConflict", "Bare-metal claim identity does not match", nil)
	}
	switch host.State {
	case "Claimed":
		if m.Spec.Bootstrap.DataSecretName == nil {
			return r.report(ctx, infra, false, "WaitingForBootstrap", "Waiting for bootstrap data Secret", nil)
		}
		var secret corev1.Secret
		if err := r.Get(ctx, types.NamespacedName{Namespace: infra.Namespace, Name: *m.Spec.Bootstrap.DataSecretName}, &secret); err != nil {
			return r.report(ctx, infra, false, "WaitingForBootstrap", "Cannot read bootstrap data Secret", err)
		}
		data := secret.Data["value"]
		if err := validateBootstrap(string(data), string(secret.Data["format"])); err != nil {
			return r.report(ctx, infra, false, "UnsupportedBootstrap", err.Error(), nil)
		}
		envelope, err := sealedseed.Seal(host.PublicKey, host.Name, id, data)
		if err != nil {
			return r.report(ctx, infra, false, "InvalidEnrollment", "Host lacks a valid enrolled bootstrap key", err)
		}
		if err := api.DeployBareMetal(ctx, id, spec.OSImageRef, envelope); err != nil {
			return r.report(ctx, infra, false, "ProvisioningFailed", "Cannot start sealed OS deployment; retry will observe the same claim", err)
		}
	case "Ready":
		providerID := "gomi:///" + id
		if infra.Spec.ProviderID == nil || *infra.Spec.ProviderID != providerID {
			before := infra.DeepCopy()
			infra.Spec.ProviderID = &providerID
			if err := r.Patch(ctx, infra, client.MergeFrom(before)); err != nil {
				return retry, err
			}
		}
		before := infra.DeepCopy()
		infra.Status.Addresses = nil
		if host.IPAddress != "" {
			infra.Status.Addresses = []infrav1.Address{{Type: "InternalIP", Address: host.IPAddress}}
		}
		if err := r.Status().Patch(ctx, infra, client.MergeFrom(before)); err != nil {
			return retry, err
		}
		return r.report(ctx, infra, true, "Provisioned", "Bare-metal OS and kubeadm bootstrap completed", nil)
	case "Failed":
		return r.report(ctx, infra, false, "ProvisioningFailed", "Physical deployment failed; inspect GOMI before replacement", nil)
	case "Releasing":
		return r.report(ctx, infra, false, "OwnershipConflict", "Claim is being released", nil)
	case "Deploying":
	default:
		return r.report(ctx, infra, false, "InvalidState", "GOMI returned an unknown claim state", nil)
	}
	return r.report(ctx, infra, false, "Provisioning", "Waiting for sealed physical-host bootstrap", nil)
}

func (r *MachineReconciler) removeBareMetal(ctx context.Context, api *gomi.Client, infra *infrav1.GomiMachine) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(infra, finalizer) {
		return ctrl.Result{}, nil
	}
	id := infra.Spec.InstanceID
	if id != "" {
		host, err := api.GetBareMetal(ctx, id)
		if err != nil && !gomi.IsStatus(err, 404) {
			return retry, err
		}
		if err == nil {
			if host.Owner != id || infra.Spec.BareMetal == nil || host.Pool != infra.Spec.BareMetal.Pool {
				return retry, fmt.Errorf("refusing to release claim with conflicting ownership")
			}
			if err := api.ReleaseBareMetal(ctx, id); err != nil {
				return retry, err
			}
			return retry, nil
		}
	}
	before := infra.DeepCopy()
	controllerutil.RemoveFinalizer(infra, finalizer)
	return ctrl.Result{}, r.Patch(ctx, infra, client.MergeFrom(before))
}
