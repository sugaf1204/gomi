package controllers

import (
	"context"
	"fmt"

	infrav1 "github.com/sugaf1204/gomi/providers/cluster-api/api/v1alpha1"
	"github.com/sugaf1204/gomi/providers/cluster-api/internal/gomi"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func ownership(id string) string { return "cluster-api GomiMachine " + id }
func ensureTemplate(ctx context.Context, api *gomi.Client, id, data string) error {
	if err := api.RequireVMSeedTemplates(ctx); err != nil {
		return err
	}
	existing, err := api.GetTemplate(ctx, id)
	if err == nil {
		if existing.Description != ownership(id) || existing.UserData != data || existing.DeliveryMode != "vm-seed" {
			return fmt.Errorf("bootstrap template ownership or content conflict")
		}
		return nil
	}
	if !gomi.IsStatus(err, 404) {
		return err
	}
	return api.CreateTemplate(ctx, gomi.Template{Name: id, UserData: data, Description: ownership(id), DeliveryMode: "vm-seed"})
}
func (r *MachineReconciler) remove(ctx context.Context, api *gomi.Client, infra *infrav1.GomiMachine) (ctrl.Result, error) {
	if infra.Spec.Kind == "BareMetal" {
		return r.removeBareMetal(ctx, api, infra)
	}
	if !controllerutil.ContainsFinalizer(infra, finalizer) {
		return ctrl.Result{}, nil
	}
	id := infra.Spec.InstanceID
	if id != "" {
		v, err := api.GetVM(ctx, id)
		if err != nil && !gomi.IsStatus(err, 404) {
			return retry, err
		}
		if err == nil {
			if !v.OwnedBy(id) {
				return retry, fmt.Errorf("refusing to delete VM with conflicting ownership")
			}
			if err := api.DeleteVM(ctx, id); err != nil {
				return retry, err
			}
			// Confirm absence; never remove bootstrap data while a VM may still use it.
			if _, err := api.GetVM(ctx, id); !gomi.IsStatus(err, 404) {
				return retry, err
			}
		}
		t, err := api.GetTemplate(ctx, id)
		if err != nil && !gomi.IsStatus(err, 404) {
			return retry, err
		}
		if err == nil {
			if t.Description != ownership(id) {
				return retry, fmt.Errorf("refusing to delete bootstrap template with conflicting ownership")
			}
			if err := api.DeleteTemplate(ctx, id); err != nil {
				return retry, err
			}
		}
	}
	before := infra.DeepCopy()
	controllerutil.RemoveFinalizer(infra, finalizer)
	return ctrl.Result{}, r.Patch(ctx, infra, client.MergeFrom(before))
}
