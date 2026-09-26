package controllers

import (
	"context"
	"fmt"
	"time"

	infrav1 "github.com/sugaf1204/gomi/providers/cluster-api/api/v1alpha1"
	"github.com/sugaf1204/gomi/providers/cluster-api/internal/gomi"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const finalizer = "gomimachine.infrastructure.cluster.x-k8s.io"

var retry = ctrl.Result{RequeueAfter: 15 * time.Second}

func paused(o client.Object) bool { _, ok := o.GetAnnotations()["cluster.x-k8s.io/paused"]; return ok }
func clusterPaused(c *clusterv1.Cluster) bool {
	return paused(c) || (c.Spec.Paused != nil && *c.Spec.Paused)
}
func ownerName(o client.Object, kind string) string {
	for _, r := range o.GetOwnerReferences() {
		if r.Kind == kind && (r.APIVersion == "cluster.x-k8s.io/v1beta1" || r.APIVersion == "cluster.x-k8s.io/v1beta2") {
			return r.Name
		}
	}
	return ""
}
func connection(ctx context.Context, c client.Reader, cluster *infrav1.GomiCluster) (*gomi.Client, error) {
	var s corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Spec.CredentialsRef.Name}, &s); err != nil {
		return nil, fmt.Errorf("read credentials: %w", err)
	}
	return gomi.New(cluster.Spec.Server, string(s.Data["token"]))
}
func condition(conditions *[]metav1.Condition, generation int64, ready bool, reason, message string) {
	status := metav1.ConditionFalse
	if ready {
		status = metav1.ConditionTrue
	}
	meta.SetStatusCondition(conditions, metav1.Condition{Type: "Ready", Status: status, ObservedGeneration: generation, Reason: reason, Message: message})
}
