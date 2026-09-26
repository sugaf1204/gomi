package integration

import (
	"context"
	"testing"
	"time"

	infrav1 "github.com/sugaf1204/gomi/providers/cluster-api/api/v1alpha1"
	"github.com/sugaf1204/gomi/providers/cluster-api/internal/controllers"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func testManagerWatches(t *testing.T, config *rest.Config, kube client.Client) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	key := types.NamespacedName{Namespace: "capi-ubuntu", Name: "infra"}
	var infra infrav1.GomiCluster
	must(t, kube.Get(ctx, key, &infra))
	infra.Status = infrav1.GomiClusterStatus{}
	must(t, kube.Status().Update(ctx, &infra))
	mgr, err := ctrl.NewManager(config, ctrl.Options{Scheme: kube.Scheme(), Cache: cache.Options{DefaultNamespaces: map[string]cache.Config{"capi-ubuntu": {}}}, Metrics: metricsserver.Options{BindAddress: "0"}, HealthProbeBindAddress: "0"})
	must(t, err)
	must(t, (&controllers.ClusterReconciler{Client: mgr.GetClient()}).SetupWithManager(mgr))
	must(t, (&controllers.MachineReconciler{Client: mgr.GetClient()}).SetupWithManager(mgr))
	done := make(chan error, 1)
	go func() { done <- mgr.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("manager failed to stop")
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		must(t, kube.Get(ctx, key, &infra))
		if infra.Status.Initialization.Provisioned != nil && *infra.Status.Initialization.Provisioned {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("manager watch did not reconcile GomiCluster")
}
