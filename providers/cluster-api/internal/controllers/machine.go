package controllers

import (
	"context"
	"fmt"
	"sigs.k8s.io/yaml"
	"strings"

	infrav1 "github.com/sugaf1204/gomi/providers/cluster-api/api/v1alpha1"
	"github.com/sugaf1204/gomi/providers/cluster-api/internal/gomi"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type MachineReconciler struct{ client.Client }

func (r *MachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var infra infrav1.GomiMachine
	if err := r.Get(ctx, req.NamespacedName, &infra); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if paused(&infra) {
		return retry, nil
	}
	owner := ownerName(&infra, "Machine")
	if owner == "" {
		return retry, nil
	}
	var machine clusterv1.Machine
	if err := r.Get(ctx, types.NamespacedName{Namespace: infra.Namespace, Name: owner}, &machine); err != nil {
		return retry, client.IgnoreNotFound(err)
	}
	var cluster clusterv1.Cluster
	if err := r.Get(ctx, types.NamespacedName{Namespace: infra.Namespace, Name: machine.Spec.ClusterName}, &cluster); err != nil {
		return retry, client.IgnoreNotFound(err)
	}
	if clusterPaused(&cluster) || paused(&machine) {
		return retry, nil
	}
	if cluster.Spec.InfrastructureRef.Kind != "GomiCluster" || cluster.Spec.InfrastructureRef.APIGroup != infrav1.GroupVersion.Group {
		return r.report(ctx, &infra, false, "InvalidCluster", "Cluster must reference a GomiCluster", nil)
	}
	var ci infrav1.GomiCluster
	if err := r.Get(ctx, types.NamespacedName{Namespace: infra.Namespace, Name: cluster.Spec.InfrastructureRef.Name}, &ci); err != nil {
		return retry, err
	}
	if paused(&ci) {
		return retry, nil
	}
	api, err := connection(ctx, r.Client, &ci)
	if err != nil {
		return r.report(ctx, &infra, false, "CredentialsUnavailable", "Cannot read GOMI credentials", err)
	}
	if !infra.DeletionTimestamp.IsZero() {
		return r.remove(ctx, api, &infra)
	}
	if infra.Spec.Kind != "" && infra.Spec.Kind != "VirtualMachine" && infra.Spec.Kind != "BareMetal" {
		return r.report(ctx, &infra, false, "UnsupportedKind", "Unsupported machine kind", nil)
	}
	// Commit identity and finalizer before ANY external writes. Identity belongs in
	// spec because status and Kubernetes UIDs do not survive clusterctl move.
	if infra.Spec.InstanceID == "" || !controllerutil.ContainsFinalizer(&infra, finalizer) {
		before := infra.DeepCopy()
		if infra.Spec.InstanceID == "" {
			infra.Spec.InstanceID = "capi-" + string(infra.UID)
		}
		controllerutil.AddFinalizer(&infra, finalizer)
		if err := r.Patch(ctx, &infra, client.MergeFrom(before)); err != nil {
			return retry, err
		}
		return retry, nil
	}
	if infra.Spec.Kind == "BareMetal" {
		return r.reconcileBareMetal(ctx, api, &infra, &machine)
	}
	if infra.Spec.VirtualMachine == nil || infra.Spec.BareMetal != nil {
		return r.report(ctx, &infra, false, "InvalidSpec", "VirtualMachine configuration is required", nil)
	}
	id := infra.Spec.InstanceID
	existing, err := api.GetVM(ctx, id)
	if err == nil {
		return r.observe(ctx, &infra, existing)
	}
	if !gomi.IsStatus(err, 404) {
		return r.report(ctx, &infra, false, "GomiUnavailable", "Cannot read GOMI VM", err)
	}
	// Never silently resurrect an instance that CAPI has already provisioned.
	// Surface disappearance for remediation by MachineHealthCheck/replacement.
	if infra.Spec.ProviderID != nil {
		return r.report(ctx, &infra, false, "InstanceMissing", "Previously provisioned VM is missing; replace the Machine", nil)
	}
	if machine.Spec.Bootstrap.DataSecretName == nil {
		return r.report(ctx, &infra, false, "WaitingForBootstrap", "Waiting for bootstrap data Secret", nil)
	}
	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: infra.Namespace, Name: *machine.Spec.Bootstrap.DataSecretName}, &secret); err != nil {
		return r.report(ctx, &infra, false, "WaitingForBootstrap", "Cannot read bootstrap data Secret", err)
	}
	data := string(secret.Data["value"])
	if err := validateBootstrap(data, string(secret.Data["format"])); err != nil {
		return r.report(ctx, &infra, false, "UnsupportedBootstrap", err.Error(), nil)
	}
	if err := ensureTemplate(ctx, api, id, data); err != nil {
		return r.report(ctx, &infra, false, "BootstrapFailed", "Cannot persist owned bootstrap template", err)
	}
	if err := api.CreateVM(ctx, id, *infra.Spec.VirtualMachine); err != nil && !gomi.IsStatus(err, 409) {
		return r.report(ctx, &infra, false, "ProvisioningFailed", "GOMI VM creation failed; retry will read the stable instance ID", err)
	}
	// A timeout or conflict may mean another reconcile already created the VM.
	// Read on the next reconcile and verify ownership before using it.
	return r.report(ctx, &infra, false, "Provisioning", "Waiting for GOMI provisioning to complete", nil)
}
func (r *MachineReconciler) observe(ctx context.Context, infra *infrav1.GomiMachine, v *gomi.VM) (ctrl.Result, error) {
	if !v.OwnedBy(infra.Spec.InstanceID) {
		return r.report(ctx, infra, false, "OwnershipConflict", "VM does not reference this Machine's bootstrap template", nil)
	}
	if v.Phase == "Error" || v.Phase == "Missing" {
		return r.report(ctx, infra, false, "ProvisioningFailed", "GOMI reports VM "+v.Phase, nil)
	}
	completed := !v.Provisioning.Active && v.Provisioning.CompletedAt != nil
	if !completed {
		return r.report(ctx, infra, false, "Provisioning", "Waiting for VM provisioning completion", nil)
	}
	providerID := "gomi:///" + infra.Spec.InstanceID
	if infra.Spec.ProviderID == nil || *infra.Spec.ProviderID != providerID {
		before := infra.DeepCopy()
		infra.Spec.ProviderID = &providerID
		if err := r.Patch(ctx, infra, client.MergeFrom(before)); err != nil {
			return retry, err
		}
	}
	before := infra.DeepCopy()
	infra.Status.Addresses = nil
	for _, ip := range v.IPAddresses {
		infra.Status.Addresses = append(infra.Status.Addresses, infrav1.Address{Type: "InternalIP", Address: ip})
	}
	if err := r.Status().Patch(ctx, infra, client.MergeFrom(before)); err != nil {
		return retry, err
	}
	if v.Phase != "Running" {
		return r.report(ctx, infra, false, "InstanceNotRunning", "Provisioned VM is not running", nil)
	}
	return r.report(ctx, infra, true, "Provisioned", "GOMI VM provisioning completed", nil)
}
func (r *MachineReconciler) report(ctx context.Context, infra *infrav1.GomiMachine, ready bool, reason, message string, cause error) (ctrl.Result, error) {
	before := infra.DeepCopy()
	// Provisioned is a latch: once initialized, transient outages must not undo it.
	if ready || (infra.Spec.ProviderID != nil && infra.Spec.InstanceID != "" && *infra.Spec.ProviderID == "gomi:///"+infra.Spec.InstanceID) {
		provisioned := true
		infra.Status.Initialization.Provisioned = &provisioned
	}
	condition(&infra.Status.Conditions, infra.Generation, ready, reason, message)
	if err := r.Status().Patch(ctx, infra, client.MergeFrom(before)); err != nil {
		return retry, err
	}
	return retry, cause
}
func (r *MachineReconciler) SetupWithManager(m ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(m).For(&infrav1.GomiMachine{}).Complete(r)
}

func validateBootstrap(data, format string) error {
	if format != "" && format != "cloud-config" {
		return fmt.Errorf("unsupported bootstrap format %q; cloud-config is required", format)
	}
	s := strings.TrimSpace(data)
	if strings.HasPrefix(s, "## template: jinja") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "## template: jinja"))
	}
	if !strings.HasPrefix(s, "#cloud-config\n") {
		return fmt.Errorf("bootstrap value must contain cloud-config; Ignition and scripts are unsupported")
	}
	var config map[string]any
	if err := yaml.Unmarshal([]byte(s), &config); err != nil || config == nil {
		return fmt.Errorf("bootstrap value must be valid cloud-config YAML with quoted Jinja expressions")
	}
	return nil
}
