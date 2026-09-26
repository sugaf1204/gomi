package controllers

import (
	"context"

	infrav1 "github.com/sugaf1204/gomi/providers/cluster-api/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ClusterReconciler struct{ client.Client }

func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var infra infrav1.GomiCluster
	if err := r.Get(ctx, req.NamespacedName, &infra); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if paused(&infra) || !infra.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	owner := ownerName(&infra, "Cluster")
	if owner == "" {
		return retry, nil
	}
	var cluster clusterv1.Cluster
	if err := r.Get(ctx, types.NamespacedName{Namespace: infra.Namespace, Name: owner}, &cluster); err != nil {
		return retry, client.IgnoreNotFound(err)
	}
	if clusterPaused(&cluster) {
		return retry, nil
	}
	before := infra.DeepCopy()
	_, err := connection(ctx, r.Client, &infra)
	ready := err == nil && infra.Spec.ControlPlaneEndpoint.Host != "" && infra.Spec.ControlPlaneEndpoint.Port > 0
	reason, message := "Provisioned", "External control plane endpoint configured"
	if !ready {
		reason, message = "ConfigurationInvalid", "Configure endpoint and credentials Secret"
	}
	if ready {
		infra.Status.Initialization.Provisioned = &ready
	}
	condition(&infra.Status.Conditions, infra.Generation, ready, reason, message)
	if patchErr := r.Status().Patch(ctx, &infra, client.MergeFrom(before)); patchErr != nil {
		return retry, patchErr
	}
	return retry, err
}
func (r *ClusterReconciler) SetupWithManager(m ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(m).For(&infrav1.GomiCluster{}).Complete(r)
}
